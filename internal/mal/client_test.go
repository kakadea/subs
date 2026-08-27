package mal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseURL(t *testing.T) {
	valid := map[string]int{
		"https://myanimelist.net/anime/2076/Kindaichi_Shounen_no_Jikenbo": 2076,
		"https://www.myanimelist.net/anime/1/Test?foo=bar":                1,
	}
	for raw, want := range valid {
		got, err := ParseURL(raw)
		if err != nil || got != want {
			t.Fatalf("ParseURL(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"http://myanimelist.net/anime/2076/test",
		"https://myanimelist.net/manga/2076/test",
		"https://myanimelist.net.evil.example/anime/2076/test",
		"https://myanimelist.net/anime/not-a-number/test",
		"https://myanimelist.net/anime/0/test",
	} {
		if _, err := ParseURL(raw); err == nil {
			t.Errorf("ParseURL(%q) should fail", raw)
		}
	}
}

func TestFetchAnime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anime/2076" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("unexpected Accept header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"mal_id":2076,"url":"https://myanimelist.net/anime/2076/Kindaichi_Shounen_no_Jikenbo","images":{"jpg":{"large_image_url":"https://cdn.myanimelist.net/images/anime/1702/120440l.jpg"}},"title":"Kindaichi Shounen no Jikenbo","episodes":148}}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL)
	got, err := client.FetchAnime(context.Background(), 2076)
	if err != nil {
		t.Fatal(err)
	}
	if got.MALID != 2076 || got.Title != "Kindaichi Shounen no Jikenbo" || got.Episodes != 148 {
		t.Fatalf("unexpected anime: %+v", got)
	}
	if got.ImageURL == "" || !strings.HasPrefix(got.ImageURL, "https://cdn.myanimelist.net/") {
		t.Fatalf("unexpected image URL: %q", got.ImageURL)
	}
}

func TestFetchAnimeRejectsUntrustedImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"mal_id":1,"url":"https://myanimelist.net/anime/1/Test","images":{"jpg":{"large_image_url":"https://evil.example/cover.jpg"}},"title":"Test","episodes":1}}`))
	}))
	defer server.Close()

	got, err := NewClient(server.Client(), server.URL).FetchAnime(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageURL != "" {
		t.Fatalf("untrusted image URL should be discarded: %q", got.ImageURL)
	}
}
