package httpapp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kakadea/subs/internal/config"
	"github.com/kakadea/subs/internal/store"
)

func TestValidateSubtitleContent(t *testing.T) {
	if err := validateSubtitleContent(strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\nOlá\n")); err != nil {
		t.Fatalf("expected text subtitle to pass: %v", err)
	}
	if err := validateSubtitleContent(strings.NewReader("\x00\x01binary")); err == nil {
		t.Fatal("expected binary content to be rejected")
	}
}

func TestValidateSourceURL(t *testing.T) {
	valid, err := validateSourceURL(" https://example.com/source?id=7 ")
	if err != nil || valid != "https://example.com/source?id=7" {
		t.Fatalf("valid source URL = %q, err %v", valid, err)
	}
	for _, value := range []string{"", "http://example.com", "//example.com/path", "https://", "https://user:pass@example.com/path", "https://example.com/a" + string(rune(10)) + "b"} {
		if _, err := validateSourceURL(value); err == nil {
			t.Errorf("validateSourceURL(%q) accepted an invalid URL", value)
		}
	}
}

func TestFormatUploadSummary(t *testing.T) {
	summary, added, failed, duplicates := formatUploadSummary([]uploadResult{
		{Filename: "one.srt", Status: "adicionada", Reason: "ok"},
		{Filename: "two.ass", Status: "duplicada", Reason: "este arquivo já foi enviado"},
		{Filename: "three.exe", Status: "falha", Reason: "extensão não permitida"},
	})
	if added != 1 || failed != 1 || duplicates != 1 {
		t.Fatalf("counts = %d/%d/%d, want 1/1/1", added, failed, duplicates)
	}
	if !strings.Contains(summary, "two.ass") || !strings.Contains(summary, "three.exe") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1024: "1.0 KB", 1024 * 1024: "1.0 MB"}
	for input, expected := range cases {
		if got := formatBytes(input); got != expected {
			t.Errorf("formatBytes(%d) = %q, want %q", input, got, expected)
		}
	}
}

func TestRenderTemplates(t *testing.T) {
	a := New(config.Config{}, store.New(nil), slog.Default())
	pages := []string{"catalog.html", "login.html", "detail.html", "admin.html", "upload.html", "account.html", "project.html", "project-admin.html", "project-new.html"}
	projectID := uint64(7)
	view := ViewData{
		Title: "Test", Query: "",
		Subtitles:        []store.Subtitle{{PublicID: "abc", Title: "Test", Format: "srt", Visibility: "public", Language: "Português", FileSize: 1024}},
		LegacySubtitles:  []store.Subtitle{{PublicID: "legacy", Title: "Legacy", Format: "srt", Visibility: "public", Language: "Português", FileSize: 1024}},
		Projects:         []store.AnimeProject{{PublicID: "project", MALID: 2076, Title: "Kindaichi", Episodes: 148, SubtitleCount: 1}},
		Project:          &store.AnimeProject{ID: projectID, PublicID: "project", MALID: 2076, Title: "Kindaichi", Episodes: 148, SubtitleCount: 1, ImageURL: "https://cdn.myanimelist.net/cover.jpg", MALURL: "https://myanimelist.net/anime/2076/Kindaichi"},
		ProjectSubtitles: []store.Subtitle{{PublicID: "abc", ProjectID: &projectID, Title: "Kindaichi", OriginalFilename: "kindaichi.srt", Format: "srt", Visibility: "public", Language: "Português", Version: "1.0", FileSize: 1024}},
		ProjectSources:   []store.ProjectSource{{PublicID: "source", ProjectID: projectID, Name: "Fonte oficial", URL: "https://example.com/source", Description: "Referência"}},
	}
	for _, page := range pages {
		recorder := httptest.NewRecorder()
		a.render(recorder, page, view)
		if recorder.Code != 200 {
			t.Errorf("render %s returned status %d", page, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), "<!doctype html>") {
			t.Errorf("render %s did not produce html", page)
		}
	}
}

func TestContentType(t *testing.T) {
	if got := contentType("srt"); got != "application/x-subrip" && got != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected srt content type: %q", got)
	}
	if got := contentType("unknown"); got != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected fallback content type: %q", got)
	}
}

func TestDynamicResponsesAreNotCached(t *testing.T) {
	a := New(config.Config{}, store.New(nil), slog.Default())
	handler := a.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/", "/p/project", "/login", "/admin"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if got := resp.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
			t.Fatalf("%s cache-control = %q", path, got)
		}
		if got := resp.Header().Get("CDN-Cache-Control"); got != "no-store" {
			t.Fatalf("%s cdn-cache-control = %q", path, got)
		}
	}
}

func TestAdminRequiresLogin(t *testing.T) {
	a := New(config.Config{SessionCookieName: "subs_session"}, store.New(nil), slog.Default())
	for _, path := range []string{"/admin", "/admin/upload", "/admin/projects/new"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		a.Handler().ServeHTTP(resp, req)
		if resp.Code != http.StatusSeeOther {
			t.Fatalf("%s status = %d, want %d", path, resp.Code, http.StatusSeeOther)
		}
		if location := resp.Header().Get("Location"); !strings.HasPrefix(location, "/login?next=") {
			t.Fatalf("%s location = %q", path, location)
		}
		if got := resp.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
			t.Fatalf("%s cache-control = %q", path, got)
		}
	}
}

func TestServeSubtitle(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "subtitles", "ab", "cd")
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "checksum.srt"), []byte("linha 1\nlinha 2\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	a := New(config.Config{StorageRoot: root}, store.New(nil), slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/download/id", nil)
	resp := httptest.NewRecorder()
	a.accelRedirect(resp, req, store.Subtitle{StoragePath: "subtitles/ab/cd/checksum.srt", Format: "srt", OriginalFilename: "anime.srt"})
	if resp.Code != http.StatusOK {
		t.Fatalf("download status = %d", resp.Code)
	}
	if got := resp.Header().Get("Content-Length"); got != "16" {
		t.Fatalf("content length = %q, want 16", got)
	}
	if got := resp.Header().Get("Content-Disposition"); !strings.Contains(got, "anime.srt") {
		t.Fatalf("content disposition = %q", got)
	}
	if got := resp.Body.String(); got != "linha 1\nlinha 2\n" {
		t.Fatalf("body = %q", got)
	}

	rangeReq := httptest.NewRequest(http.MethodGet, "/download/id", nil)
	rangeReq.Header.Set("Range", "bytes=0-6")
	rangeResp := httptest.NewRecorder()
	a.accelRedirect(rangeResp, rangeReq, store.Subtitle{StoragePath: "subtitles/ab/cd/checksum.srt", Format: "srt", OriginalFilename: "anime.srt"})
	if rangeResp.Code != http.StatusPartialContent || rangeResp.Body.String() != "linha 1" {
		t.Fatalf("range response = status %d body %q", rangeResp.Code, rangeResp.Body.String())
	}
}
