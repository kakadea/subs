package httpapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kakadea/subs/internal/config"
	"github.com/kakadea/subs/internal/mal"
	"github.com/kakadea/subs/internal/store"
	"github.com/kakadea/subs/internal/webassets"
)

type App struct {
	cfg      config.Config
	store    *store.Store
	metadata mal.MetadataProvider
	log      *slog.Logger
}

type ViewData struct {
	Title            string
	User             *store.User
	CSRF             string
	Query            string
	Subtitles        []store.Subtitle
	Projects         []store.AnimeProject
	Project          *store.AnimeProject
	ProjectSubtitles []store.Subtitle
	LegacySubtitles  []store.Subtitle
	ProjectSources   []store.ProjectSource
	ProjectTab       string
	UploadSummary    string
	Subtitle         *store.Subtitle
	Error            string
	Success          string
	Link             string
	PublicLink       string
	MaxUploadMB      int64
	MaxUploadFiles   int
	MaxUploadBatchMB int64
}

var allowedExtensions = map[string]bool{
	".srt": true,
	".ass": true,
	".ssa": true,
	".vtt": true,
	".sub": true,
}

func New(cfg config.Config, st *store.Store, logger *slog.Logger, providers ...mal.MetadataProvider) *App {
	if logger == nil {
		logger = slog.Default()
	}
	var metadata mal.MetadataProvider
	if len(providers) > 0 {
		metadata = providers[0]
	}
	if metadata == nil {
		metadata = mal.NewClient(nil, "")
	}
	return &App{cfg: cfg, store: st, metadata: metadata, log: logger}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	staticFS, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /", a.catalog)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("GET /admin/account", a.accountPage)
	mux.HandleFunc("POST /admin/account/password", a.changePassword)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /s/{id}", a.detail)
	mux.HandleFunc("GET /p/{id}", a.project)
	mux.HandleFunc("GET /download/{id}", a.download)
	mux.HandleFunc("GET /l/{token}", a.temporaryDownload)
	mux.HandleFunc("GET /admin", a.admin)
	mux.HandleFunc("GET /admin/projects/new", a.newProjectPage)
	mux.HandleFunc("POST /admin/projects", a.createProject)
	mux.HandleFunc("GET /admin/projects/{id}", a.adminProject)
	mux.HandleFunc("GET /admin/projects/{id}/upload", a.uploadPage)
	mux.HandleFunc("POST /admin/projects/{id}/upload", a.upload)
	mux.HandleFunc("POST /admin/projects/{id}/visibility", a.setProjectVisibility)
	mux.HandleFunc("POST /admin/projects/{id}/sources", a.createProjectSource)
	mux.HandleFunc("POST /admin/sources/{id}/delete", a.deleteProjectSource)

	mux.HandleFunc("GET /admin/upload", a.legacyUploadRedirect)
	mux.HandleFunc("POST /admin/upload", a.legacyUploadRedirect)
	mux.HandleFunc("POST /admin/subtitles/{id}/delete", a.deleteSubtitle)
	mux.HandleFunc("POST /admin/subtitles/{id}/link", a.createLink)
	return a.middleware(mux)
}

func (a *App) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				a.log.Error("panic recovered", "panic", recovered, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data: https://cdn.myanimelist.net; form-action 'self'; base-uri 'self'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/admin") || r.URL.Path == "/login" {
			w.Header().Set("Cache-Control", "no-store")
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		a.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", "completed", "duration_ms", time.Since(start).Milliseconds())
	})
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.DB.PingContext(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

func (a *App) catalog(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	user, logged := a.currentUser(r)
	var userPtr *store.User
	if logged {
		userPtr = &user
	}
	projects, err := a.store.ListProjects(r.Context(), query, false)
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, "catalog.html", ViewData{Title: "Catálogo", User: userPtr, Query: query, Projects: projects})
}

func (a *App) project(w http.ResponseWriter, r *http.Request) {
	user, logged := a.currentUser(r)
	includePrivate := logged && user.IsAdmin()
	project, err := a.store.GetProject(r.Context(), r.PathValue("id"), includePrivate)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	subs, err := a.store.ListProjectSubtitles(r.Context(), project.ID, includePrivate)
	if err != nil {
		a.serverError(w, err)
		return
	}
	sources, err := a.store.ListProjectSources(r.Context(), project.ID, includePrivate)
	if err != nil {
		a.serverError(w, err)
		return
	}
	var userPtr *store.User
	if logged {
		userPtr = &user
	}
	tab := r.URL.Query().Get("tab")
	if tab != "sources" {
		tab = "subtitles"
	}
	a.render(w, "project.html", ViewData{Title: project.Title, User: userPtr, Project: &project, ProjectSubtitles: subs, ProjectSources: sources, ProjectTab: tab})
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	a.render(w, "login.html", ViewData{Title: "Entrar", Success: r.URL.Query().Get("success")})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.render(w, "login.html", ViewData{Title: "Entrar", Error: "Formulário inválido."})
		return
	}
	user, err := a.store.Authenticate(r.Context(), r.FormValue("email"), r.FormValue("password"))
	if err != nil {
		a.render(w, "login.html", ViewData{Title: "Entrar", Error: "E-mail ou senha inválidos."})
		return
	}
	token, err := a.store.CreateSession(r.Context(), user.ID, time.Now().UTC().Add(a.cfg.SessionTTL))
	if err != nil {
		a.serverError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.cfg.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(a.cfg.SessionTTL),
		MaxAge:   int(a.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	_ = a.store.Audit(r.Context(), &user.ID, "login", nil, clientIP(r), "")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(a.cfg.SessionCookieName); err == nil {
		_ = a.store.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: a.cfg.SessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, logged := a.currentUser(r)
	includePrivate := logged && user.IsAdmin()
	sub, err := a.store.GetSubtitle(r.Context(), id, includePrivate)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	var userPtr *store.User
	if logged {
		userPtr = &user
	}
	a.render(w, "detail.html", ViewData{Title: sub.Title, User: userPtr, Subtitle: &sub})
}

func (a *App) download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, logged := a.currentUser(r)
	includePrivate := logged && user.IsAdmin()
	sub, err := a.store.GetSubtitle(r.Context(), id, includePrivate)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	var userID *uint64
	if logged {
		userID = &user.ID
	}
	_ = a.store.Audit(r.Context(), userID, "download", &sub.ID, clientIP(r), `{"kind":"public"}`)
	a.accelRedirect(w, r, sub)
}

func (a *App) temporaryDownload(w http.ResponseWriter, r *http.Request) {
	link, err := a.store.GetDownloadLink(r.Context(), r.PathValue("token"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	sub, err := a.store.GetSubtitleByID(r.Context(), link.SubtitleID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), nil, "download", &sub.ID, clientIP(r), `{"kind":"temporary_link"}`)
	a.accelRedirect(w, r, sub)
}

func (a *App) admin(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	projects, err := a.store.ListProjects(r.Context(), query, true)
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, "admin.html", ViewData{Title: "Painel", User: &user, CSRF: a.ensureCSRF(w, r), Query: query, Projects: projects, Success: r.URL.Query().Get("success"), Link: r.URL.Query().Get("link")})
}

func (a *App) legacyUploadRedirect(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	http.Redirect(w, r, "/admin/projects/new", http.StatusSeeOther)
}

func (a *App) setProjectVisibility(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if !a.validCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	publicID := r.PathValue("id")
	project, err := a.store.GetProject(r.Context(), publicID, true)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	visibility := "private"
	message := "Projeto retirado do catálogo público."
	if r.FormValue("visibility") == "public" {
		visibility = "public"
		message = "Projeto compartilhado no catálogo público."
	}
	if err := a.store.SetProjectVisibility(r.Context(), publicID, visibility); err != nil {
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), &user.ID, "project_visibility", nil, clientIP(r), fmt.Sprintf(`{"project_id":%d,"visibility":%q}`, project.ID, visibility))
	http.Redirect(w, r, "/admin/projects/"+project.PublicID+"?success="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *App) createProjectSource(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if !a.validCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	publicID := r.PathValue("id")
	project, err := a.store.GetProject(r.Context(), publicID, true)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	name := limitString(r.FormValue("name"), 160)
	sourceURL, err := validateSourceURL(r.FormValue("url"))
	if err != nil {
		a.redirectProjectError(w, r, project.PublicID, err.Error())
		return
	}
	if name == "" {
		a.redirectProjectError(w, r, project.PublicID, "Informe o nome da fonte.")
		return
	}
	publicSourceID, err := randomID()
	if err != nil {
		a.serverError(w, err)
		return
	}
	source := store.ProjectSource{ProjectID: project.ID, PublicID: publicSourceID, Name: name, URL: sourceURL, Description: limitString(r.FormValue("description"), 500), CreatedBy: user.ID}
	if err := a.store.CreateProjectSource(r.Context(), source); err != nil {
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), &user.ID, "source_create", nil, clientIP(r), fmt.Sprintf(`{"project_id":%d}`, project.ID))
	http.Redirect(w, r, "/admin/projects/"+project.PublicID+"?success="+url.QueryEscape("Fonte adicionada ao projeto."), http.StatusSeeOther)
}

func (a *App) deleteProjectSource(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if !a.validCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	source, err := a.store.GetProjectSource(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, "/admin?success="+url.QueryEscape("Fonte já removida ou inexistente."), http.StatusSeeOther)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	project, err := a.store.GetProjectByID(r.Context(), source.ProjectID)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if err := a.store.DeleteProjectSource(r.Context(), source.PublicID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Redirect(w, r, "/admin/projects/"+project.PublicID+"?success="+url.QueryEscape("Fonte já removida ou inexistente."), http.StatusSeeOther)
			return
		}
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), &user.ID, "source_delete", nil, clientIP(r), fmt.Sprintf(`{"project_id":%d}`, project.ID))
	http.Redirect(w, r, "/admin/projects/"+project.PublicID+"?success="+url.QueryEscape("Fonte removida."), http.StatusSeeOther)
}

func (a *App) redirectProjectError(w http.ResponseWriter, r *http.Request, publicID, message string) {
	http.Redirect(w, r, "/admin/projects/"+publicID+"?error="+url.QueryEscape(message), http.StatusSeeOther)
}

func (a *App) newProjectPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	a.render(w, "project-new.html", ViewData{Title: "Novo projeto", User: &user, CSRF: a.ensureCSRF(w, r)})
}

func (a *App) createProject(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.render(w, "project-new.html", ViewData{Title: "Novo projeto", User: &user, CSRF: a.ensureCSRF(w, r), Error: "Formulário inválido."})
		return
	}
	if !a.validCSRF(r) {
		a.render(w, "project-new.html", ViewData{Title: "Novo projeto", User: &user, CSRF: a.ensureCSRF(w, r), Error: "A sessão do formulário expirou. Recarregue a página e tente novamente."})
		return
	}
	malURL := strings.TrimSpace(r.FormValue("mal_url"))
	malID, err := mal.ParseURL(malURL)
	if err != nil {
		a.render(w, "project-new.html", ViewData{Title: "Novo projeto", User: &user, CSRF: a.ensureCSRF(w, r), Error: err.Error()})
		return
	}
	if existing, err := a.store.GetProjectByMALID(r.Context(), malID); err == nil {
		http.Redirect(w, r, "/admin/projects/"+existing.PublicID, http.StatusSeeOther)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, err)
		return
	}
	metadata, err := a.metadata.FetchAnime(r.Context(), malID)
	if err != nil {
		a.render(w, "project-new.html", ViewData{Title: "Novo projeto", User: &user, CSRF: a.ensureCSRF(w, r), Error: "Não foi possível coletar os dados desse anime agora. Confira a URL e tente novamente."})
		return
	}
	publicID, err := randomID()
	if err != nil {
		a.serverError(w, err)
		return
	}
	project := store.AnimeProject{PublicID: publicID, MALID: metadata.MALID, MALURL: metadata.MALURL, Title: metadata.Title, ImageURL: metadata.ImageURL, Episodes: metadata.Episodes, CreatedBy: user.ID}
	if err := a.store.CreateProject(r.Context(), project); err != nil {
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), &user.ID, "project_create", nil, clientIP(r), fmt.Sprintf(`{"mal_id":%d}`, project.MALID))
	http.Redirect(w, r, "/admin/projects/"+project.PublicID+"?success="+url.QueryEscape("Projeto criado e metadados coletados."), http.StatusSeeOther)
}

func (a *App) adminProject(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	project, err := a.store.GetProject(r.Context(), r.PathValue("id"), true)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	subs, err := a.store.ListProjectSubtitles(r.Context(), project.ID, true)
	if err != nil {
		a.serverError(w, err)
		return
	}
	sources, err := a.store.ListProjectSources(r.Context(), project.ID, true)
	if err != nil {
		a.serverError(w, err)
		return
	}
	publicLink := ""
	if project.Visibility == "public" {
		publicLink = strings.TrimRight(a.cfg.BaseURL, "/") + "/p/" + project.PublicID
	}
	a.render(w, "project-admin.html", ViewData{Title: project.Title, User: &user, Project: &project, ProjectSubtitles: subs, ProjectSources: sources, CSRF: a.ensureCSRF(w, r), Success: r.URL.Query().Get("success"), Error: r.URL.Query().Get("error"), UploadSummary: r.URL.Query().Get("summary"), Link: r.URL.Query().Get("link"), PublicLink: publicLink, MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024})

}

func (a *App) accountPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	a.render(w, "account.html", ViewData{Title: "Conta", User: &user, CSRF: a.ensureCSRF(w, r)})
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.render(w, "account.html", ViewData{Title: "Conta", User: &user, CSRF: a.ensureCSRF(w, r), Error: "Formulário inválido."})
		return
	}
	if !a.validCSRF(r) {
		a.render(w, "account.html", ViewData{Title: "Conta", User: &user, CSRF: a.ensureCSRF(w, r), Error: "A sessão do formulário expirou. Recarregue a página e tente novamente."})
		return
	}
	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmation := r.FormValue("new_password_confirmation")
	if len(newPassword) < 12 {
		a.render(w, "account.html", ViewData{Title: "Conta", User: &user, CSRF: a.ensureCSRF(w, r), Error: "A nova senha precisa ter pelo menos 12 caracteres."})
		return
	}
	if newPassword != confirmation {
		a.render(w, "account.html", ViewData{Title: "Conta", User: &user, CSRF: a.ensureCSRF(w, r), Error: "A confirmação da nova senha não confere."})
		return
	}
	if _, err := a.store.Authenticate(r.Context(), user.Email, currentPassword); err != nil {
		a.render(w, "account.html", ViewData{Title: "Conta", User: &user, CSRF: a.ensureCSRF(w, r), Error: "A senha atual está incorreta."})
		return
	}
	if err := a.store.SetAdminPassword(r.Context(), user.Email, newPassword); err != nil {
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), &user.ID, "password_change", nil, clientIP(r), "")
	clearCookie := func(name string) {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name == a.cfg.SessionCookieName, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteStrictMode})
	}
	clearCookie(a.cfg.SessionCookieName)
	clearCookie("subs_csrf")
	http.Redirect(w, r, "/login?success="+url.QueryEscape("Senha alterada. Faça login novamente."), http.StatusSeeOther)
}

func (a *App) uploadPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	project, err := a.store.GetProject(r.Context(), r.PathValue("id"), true)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, "upload.html", ViewData{Title: "Adicionar legenda", User: &user, Project: &project, CSRF: a.ensureCSRF(w, r), MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024, MaxUploadFiles: a.cfg.MaxUploadFiles, MaxUploadBatchMB: a.cfg.MaxUploadBatchBytes / 1024 / 1024})
}

func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	projectID := r.PathValue("id")
	if projectID == "" {
		http.Redirect(w, r, "/admin/projects/new", http.StatusSeeOther)
		return
	}
	project, err := a.store.GetProject(r.Context(), projectID, true)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	batchLimit := a.cfg.MaxUploadBatchBytes + 1024*1024
	r.Body = http.MaxBytesReader(w, r.Body, batchLimit)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		a.uploadError(w, r, user, &project, "O lote excede o limite total permitido ou é inválido.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if !a.validCSRF(r) {
		a.uploadError(w, r, user, &project, "A sessão do formulário expirou. Recarregue a página e tente novamente.")
		return
	}
	files := r.MultipartForm.File["subtitle"]
	if len(files) == 0 {
		a.uploadError(w, r, user, &project, "Selecione pelo menos um arquivo de legenda.")
		return
	}
	if len(files) > a.cfg.MaxUploadFiles {
		a.uploadError(w, r, user, &project, fmt.Sprintf("Selecione no máximo %d arquivos por lote.", a.cfg.MaxUploadFiles))
		return
	}
	language := limitString(defaultString(r.FormValue("language"), "Português"), 64)
	version := limitString(defaultString(r.FormValue("version"), "1.0"), 64)
	visibility := "public"
	if r.FormValue("visibility") == "private" {
		visibility = "private"
	}
	results := make([]uploadResult, 0, len(files))
	for _, header := range files {
		result := a.persistUploadedSubtitle(r.Context(), user, project, header, language, version, visibility, clientIP(r))
		results = append(results, result)
	}
	summary, added, failed, duplicates := formatUploadSummary(results)
	message := fmt.Sprintf("Lote concluído: %d legenda(s) adicionada(s).", added)
	if failed > 0 || duplicates > 0 {
		message += fmt.Sprintf(" %d falha(s), %d duplicada(s).", failed, duplicates)
	}
	redirect := "/admin/projects/" + project.PublicID + "?success=" + url.QueryEscape(message)
	if summary != "" {
		redirect += "&summary=" + url.QueryEscape(summary)
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

type uploadResult struct {
	Filename string
	Status   string
	Reason   string
}

func (a *App) persistUploadedSubtitle(ctx context.Context, user store.User, project store.AnimeProject, header *multipart.FileHeader, language, version, visibility, ip string) uploadResult {
	result := uploadResult{Filename: limitString(filepath.Base(header.Filename), 120)}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		result.Status = "falha"
		result.Reason = "extensão não permitida"
		return result
	}
	if header.Size <= 0 {
		result.Status = "falha"
		result.Reason = "arquivo vazio"
		return result
	}
	if header.Size > a.cfg.MaxUploadBytes {
		result.Status = "falha"
		result.Reason = fmt.Sprintf("ultrapassa %d MB", a.cfg.MaxUploadBytes/1024/1024)
		return result
	}
	file, err := header.Open()
	if err != nil {
		result.Status = "falha"
		result.Reason = "não foi possível abrir o arquivo"
		return result
	}
	defer file.Close()
	storageRoot := filepath.Clean(a.cfg.StorageRoot)
	tempDir := filepath.Join(storageRoot, "temp")
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		result.Status = "falha"
		result.Reason = "não foi possível preparar o storage"
		return result
	}
	tmp, err := os.CreateTemp(tempDir, "upload-*")
	if err != nil {
		result.Status = "falha"
		result.Reason = "não foi possível preparar o arquivo"
		return result
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hasher := sha256.New()
	validator := &subtitleContentValidator{}
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher, validator), io.LimitReader(file, a.cfg.MaxUploadBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		result.Status = "falha"
		result.Reason = "erro ao ler o arquivo"
		return result
	}
	if validator.invalid {
		result.Status = "falha"
		result.Reason = "conteúdo binário ou inválido"
		return result
	}
	if written <= 0 {
		result.Status = "falha"
		result.Reason = "arquivo vazio"
		return result
	}
	if written > a.cfg.MaxUploadBytes {
		result.Status = "falha"
		result.Reason = fmt.Sprintf("ultrapassa %d MB", a.cfg.MaxUploadBytes/1024/1024)
		return result
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	relativePath := filepath.ToSlash(filepath.Join("subtitles", checksum[:2], checksum[2:4], checksum+ext))
	finalPath := filepath.Join(storageRoot, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o750); err != nil {
		result.Status = "falha"
		result.Reason = "não foi possível preparar o destino"
		return result
	}
	if err := os.Link(tmpName, finalPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			result.Status = "duplicada"
			result.Reason = "este arquivo já foi enviado"
			return result
		}
		result.Status = "falha"
		result.Reason = "não foi possível gravar no storage"
		return result
	}
	publicID, err := randomID()
	if err != nil {
		_ = os.Remove(finalPath)
		result.Status = "falha"
		result.Reason = "não foi possível gerar o identificador"
		return result
	}
	projectID := project.ID
	sub := store.Subtitle{ProjectID: &projectID, PublicID: publicID, Title: project.Title, Language: language, Format: strings.TrimPrefix(ext, "."), OriginalFilename: limitString(filepath.Base(header.Filename), 255), StorageName: checksum + ext, StoragePath: relativePath, FileSize: written, Checksum: checksum, Version: version, Visibility: visibility, CreatedBy: user.ID}
	if err := a.store.CreateSubtitle(ctx, sub); err != nil {
		_ = os.Remove(finalPath)
		result.Status = "falha"
		result.Reason = "não foi possível salvar no banco"
		return result
	}
	_ = a.store.Audit(ctx, &user.ID, "upload", &sub.ID, ip, fmt.Sprintf(`{"filename":"stored","project_id":%d}`, project.ID))
	result.Status = "adicionada"
	result.Reason = "ok"
	return result
}

func (a *App) deleteSubtitle(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if !a.validCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	sub, err := a.store.GetSubtitle(r.Context(), r.PathValue("id"), true)
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, "/admin?success="+url.QueryEscape("Legenda já removida ou inexistente."), http.StatusSeeOther)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	projectPublicID := ""
	if sub.ProjectID != nil {
		if project, err := a.store.GetProjectByID(r.Context(), *sub.ProjectID); err == nil {
			projectPublicID = project.PublicID
		}
	}
	storageRoot := filepath.Clean(a.cfg.StorageRoot)
	storedPath := filepath.Clean(filepath.FromSlash(sub.StoragePath))
	filePath := filepath.Join(storageRoot, storedPath)
	if filePath == storageRoot || !strings.HasPrefix(filePath, storageRoot+string(os.PathSeparator)) {
		a.serverError(w, fmt.Errorf("invalid storage path"))
		return
	}
	if err := a.store.DeleteSubtitle(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Redirect(w, r, "/admin?success="+url.QueryEscape("Legenda já removida ou inexistente."), http.StatusSeeOther)
			return
		}
		a.serverError(w, err)
		return
	}
	if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.log.Error("subtitle file removal failed", "path", sub.StoragePath, "error", err)
	}
	_ = a.store.Audit(r.Context(), &user.ID, "delete", &sub.ID, clientIP(r), `{"permanent":true}`)
	redirect := "/admin?success=" + url.QueryEscape("Legenda removida permanentemente.")
	if projectPublicID != "" {
		redirect = "/admin/projects/" + projectPublicID + "?success=" + url.QueryEscape("Legenda removida permanentemente.")
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *App) createLink(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if !a.validCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	sub, err := a.store.GetSubtitle(r.Context(), r.PathValue("id"), true)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	token, err := a.store.CreateDownloadLink(r.Context(), sub.ID, user.ID, time.Now().UTC().Add(a.cfg.DownloadLinkTTL))
	if err != nil {
		a.serverError(w, err)
		return
	}
	link := a.cfg.BaseURL + "/l/" + token
	redirect := "/admin?link=" + url.QueryEscape(link)
	if sub.ProjectID != nil {
		if project, err := a.store.GetProjectByID(r.Context(), *sub.ProjectID); err == nil {
			redirect = "/admin/projects/" + project.PublicID + "?link=" + url.QueryEscape(link)
		}
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *App) accelRedirect(w http.ResponseWriter, r *http.Request, sub store.Subtitle) {
	storageRoot := filepath.Clean(a.cfg.StorageRoot)
	relativePath := filepath.Clean(filepath.FromSlash(sub.StoragePath))
	filePath := filepath.Join(storageRoot, relativePath)
	prefix := storageRoot + string(os.PathSeparator)
	if filePath == storageRoot || !strings.HasPrefix(filePath, prefix) {
		a.serverError(w, fmt.Errorf("invalid storage path"))
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		a.serverError(w, err)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		a.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType(sub.Format))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": sub.OriginalFilename}))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, sub.OriginalFilename, stat.ModTime(), file)
}

func (a *App) currentUser(r *http.Request) (store.User, bool) {
	cookie, err := r.Cookie(a.cfg.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return store.User{}, false
	}
	user, err := a.store.GetSessionUser(r.Context(), cookie.Value)
	return user, err == nil
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	user, ok := a.currentUser(r)
	if !ok || !user.IsAdmin() {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return store.User{}, false
	}
	return user, true
}

func (a *App) render(w http.ResponseWriter, name string, data ViewData) {
	tmpl, err := template.New("base.html").Funcs(template.FuncMap{
		"bytes": formatBytes,
		"date":  func(t time.Time) string { return t.Local().Format("02/01/2006 15:04") },
		"lower": strings.ToLower,
		"trim":  strings.TrimSpace,
	}).ParseFS(webassets.Files, "templates/base.html", "templates/"+name)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		a.log.Error("template render failed", "error", err)
	}
}

func (a *App) uploadError(w http.ResponseWriter, r *http.Request, user store.User, project *store.AnimeProject, message string) {
	a.render(w, "upload.html", ViewData{Title: "Adicionar legenda", User: &user, Project: project, CSRF: a.ensureCSRF(w, r), Error: message, MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024, MaxUploadFiles: a.cfg.MaxUploadFiles, MaxUploadBatchMB: a.cfg.MaxUploadBatchBytes / 1024 / 1024})
}

func (a *App) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie("subs_csrf"); err == nil && len(cookie.Value) >= 32 {
		return cookie.Value
	}
	token, err := randomCSRF()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: "subs_csrf", Value: token, Path: "/", HttpOnly: false, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: int(a.cfg.SessionTTL.Seconds())})
	return token
}

func (a *App) validCSRF(r *http.Request) bool {
	cookie, err := r.Cookie("subs_csrf")
	if err != nil || len(cookie.Value) < 32 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.FormValue("_csrf"))) == 1
}

func (a *App) serverError(w http.ResponseWriter, err error) {
	a.log.Error("server error", "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

type subtitleContentValidator struct {
	checked bool
	invalid bool
}

func (v *subtitleContentValidator) Write(p []byte) (int, error) {
	if !v.checked {
		v.checked = true
		if bytes.IndexByte(p, 0) >= 0 {
			v.invalid = true
		}
	}
	return len(p), nil
}

func formatUploadSummary(results []uploadResult) (string, int, int, int) {
	var lines []string
	added, failed, duplicates := 0, 0, 0
	for _, result := range results {
		switch result.Status {
		case "adicionada":
			added++
		case "duplicada":
			duplicates++
		default:
			failed++
		}
		if result.Status != "adicionada" {
			lines = append(lines, result.Filename+": "+result.Reason)
		}
	}
	return strings.Join(lines, " | "), added, failed, duplicates
}

func validateSubtitleContent(file io.Reader) error {
	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if bytes.IndexByte(buf[:n], 0) >= 0 {
		return fmt.Errorf("binary content")
	}
	return nil
}

func randomCSRF() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func validateSourceURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("informe a URL da fonte")
	}
	if len([]rune(value)) > 1024 || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("a URL da fonte é muito longa ou inválida")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("a fonte precisa ser uma URL HTTPS válida")
	}
	return value, nil
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return limitString(r.RemoteAddr, 45)
}

func contentType(format string) string {
	if value := mime.TypeByExtension("." + strings.ToLower(format)); value != "" {
		return value
	}
	return "text/plain; charset=utf-8"
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KB", "MB", "GB"}
	amount := float64(value)
	for _, unit := range units {
		amount /= 1024
		if amount < 1024 || unit == "GB" {
			return fmt.Sprintf("%.1f %s", amount, unit)
		}
	}
	return fmt.Sprintf("%d B", value)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func limitString(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
