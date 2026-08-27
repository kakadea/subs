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
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kakadea/subs/internal/config"
	"github.com/kakadea/subs/internal/store"
	"github.com/kakadea/subs/internal/webassets"
)

type App struct {
	cfg   config.Config
	store *store.Store
	log   *slog.Logger
}

type ViewData struct {
	Title       string
	User        *store.User
	CSRF        string
	Query       string
	Subtitles   []store.Subtitle
	Subtitle    *store.Subtitle
	Error       string
	Success     string
	Link        string
	MaxUploadMB int64
}

var allowedExtensions = map[string]bool{
	".srt": true,
	".ass": true,
	".ssa": true,
	".vtt": true,
	".sub": true,
}

func New(cfg config.Config, st *store.Store, logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	return &App{cfg: cfg, store: st, log: logger}
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
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /s/{id}", a.detail)
	mux.HandleFunc("GET /download/{id}", a.download)
	mux.HandleFunc("GET /l/{token}", a.temporaryDownload)
	mux.HandleFunc("GET /admin", a.admin)
	mux.HandleFunc("GET /admin/upload", a.uploadPage)
	mux.HandleFunc("POST /admin/upload", a.upload)
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data:; form-action 'self'; base-uri 'self'; frame-ancestors 'none'")
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
	subs, err := a.store.ListSubtitles(r.Context(), query, logged && user.IsAdmin())
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, "catalog.html", ViewData{Title: "Catálogo", User: userPtr, Query: query, Subtitles: subs})
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	a.render(w, "login.html", ViewData{Title: "Entrar"})
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
	a.accelRedirect(w, sub)
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
	a.accelRedirect(w, sub)
}

func (a *App) admin(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	subs, err := a.store.ListSubtitles(r.Context(), query, true)
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, "admin.html", ViewData{Title: "Painel", User: &user, CSRF: a.ensureCSRF(w, r), Query: query, Subtitles: subs, Success: r.URL.Query().Get("success"), Link: r.URL.Query().Get("link"), MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024})
}

func (a *App) uploadPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	a.render(w, "upload.html", ViewData{Title: "Enviar legenda", User: &user, CSRF: a.ensureCSRF(w, r), MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024})
}

func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxUploadBytes+1024*1024)
	if err := r.ParseMultipartForm(a.cfg.MaxUploadBytes + 1024*1024); err != nil {
		a.render(w, "upload.html", ViewData{Title: "Enviar legenda", User: &user, Error: "O upload excede o limite permitido ou é inválido.", MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024})
		return
	}
	if !a.validCSRF(r) {
		a.uploadError(w, user, "A sessão do formulário expirou. Recarregue a página e tente novamente.")
		return
	}
	file, header, err := r.FormFile("subtitle")
	if err != nil {
		a.render(w, "upload.html", ViewData{Title: "Enviar legenda", User: &user, Error: "Selecione um arquivo de legenda.", MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		a.uploadError(w, user, "Extensão não permitida. Use SRT, ASS, SSA, VTT ou SUB.")
		return
	}
	if header.Size <= 0 || header.Size > a.cfg.MaxUploadBytes {
		a.uploadError(w, user, "O arquivo está vazio ou ultrapassa o limite configurado.")
		return
	}
	if err := validateSubtitleContent(file); err != nil {
		a.uploadError(w, user, "O arquivo não parece ser uma legenda de texto válida.")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		a.serverError(w, err)
		return
	}

	storageRoot := filepath.Clean(a.cfg.StorageRoot)
	tempDir := filepath.Join(storageRoot, "temp")
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		a.serverError(w, err)
		return
	}
	tmp, err := os.CreateTemp(tempDir, "upload-*")
	if err != nil {
		a.serverError(w, err)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), file)
	closeErr := tmp.Close()
	if err != nil {
		a.serverError(w, fmt.Errorf("store upload: %w", err))
		return
	}
	if closeErr != nil {
		a.serverError(w, fmt.Errorf("close upload: %w", closeErr))
		return
	}
	if written <= 0 || written > a.cfg.MaxUploadBytes {
		a.uploadError(w, user, "O arquivo ultrapassa o limite configurado.")
		return
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	relativePath := filepath.ToSlash(filepath.Join("subtitles", checksum[:2], checksum[2:4], checksum+ext))
	finalPath := filepath.Join(storageRoot, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o750); err != nil {
		a.serverError(w, err)
		return
	}
	// Hard-linking is atomic and fails safely when the same checksum is uploaded concurrently.
	if err := os.Link(tmpName, finalPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			a.uploadError(w, user, "Este arquivo já foi enviado anteriormente.")
			return
		}
		a.serverError(w, err)
		return
	}

	publicID, err := randomID()
	if err != nil {
		_ = os.Remove(finalPath)
		a.serverError(w, err)
		return
	}
	visibility := "public"
	if r.FormValue("visibility") == "private" {
		visibility = "private"
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(header.Filename), ext)
	}
	sub := store.Subtitle{
		PublicID:         publicID,
		Title:            limitString(title, 255),
		Episode:          limitString(strings.TrimSpace(r.FormValue("episode")), 64),
		Season:           limitString(strings.TrimSpace(r.FormValue("season")), 64),
		Language:         limitString(defaultString(r.FormValue("language"), "Português"), 64),
		Format:           strings.TrimPrefix(ext, "."),
		OriginalFilename: limitString(filepath.Base(header.Filename), 255),
		StorageName:      checksum + ext,
		StoragePath:      relativePath,
		FileSize:         written,
		Checksum:         checksum,
		Version:          limitString(defaultString(r.FormValue("version"), "1.0"), 64),
		Visibility:       visibility,
		CreatedBy:        user.ID,
	}
	if err := a.store.CreateSubtitle(r.Context(), sub); err != nil {
		_ = os.Remove(finalPath)
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), &user.ID, "upload", &sub.ID, clientIP(r), `{"filename":"stored"}`)
	http.Redirect(w, r, "/admin?success="+url.QueryEscape("Legenda enviada com sucesso."), http.StatusSeeOther)
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
	if err := a.store.DeleteSubtitle(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.serverError(w, err)
		return
	}
	_ = a.store.Audit(r.Context(), &user.ID, "delete", nil, clientIP(r), "")
	http.Redirect(w, r, "/admin?success="+url.QueryEscape("Legenda removida."), http.StatusSeeOther)
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
	http.Redirect(w, r, "/admin?link="+url.QueryEscape(link), http.StatusSeeOther)
}

func (a *App) accelRedirect(w http.ResponseWriter, sub store.Subtitle) {
	w.Header().Set("Content-Type", contentType(sub.Format))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": sub.OriginalFilename}))
	w.Header().Set("X-Accel-Redirect", "/protected/"+strings.TrimPrefix(filepath.ToSlash(sub.StoragePath), "/"))
	w.WriteHeader(http.StatusOK)
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

func (a *App) uploadError(w http.ResponseWriter, user store.User, message string) {
	a.render(w, "upload.html", ViewData{Title: "Enviar legenda", User: &user, Error: message, MaxUploadMB: a.cfg.MaxUploadBytes / 1024 / 1024})
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
