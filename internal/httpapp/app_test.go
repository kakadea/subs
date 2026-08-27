package httpapp

import (
	"log/slog"
	"net/http/httptest"
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
	pages := []string{"catalog.html", "login.html", "detail.html", "admin.html", "upload.html", "account.html"}
	for _, page := range pages {
		recorder := httptest.NewRecorder()
		a.render(recorder, page, ViewData{Title: "Test", Query: "", Subtitles: []store.Subtitle{{PublicID: "abc", Title: "Test", Format: "srt", Visibility: "public", Language: "Português", FileSize: 1024}}})
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
