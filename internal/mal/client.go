package mal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Anime struct {
	MALID    int
	MALURL   string
	Title    string
	ImageURL string
	Episodes int
}

type MetadataProvider interface {
	FetchAnime(ctx context.Context, malID int) (Anime, error)
}

type Client struct {
	BaseURL     string
	FallbackURL string
	HTTPClient  *http.Client
}

type response struct {
	Data struct {
		MALID  int    `json:"mal_id"`
		URL    string `json:"url"`
		Title  string `json:"title"`
		Images struct {
			JPG struct {
				Large string `json:"large_image_url"`
				Image string `json:"image_url"`
			} `json:"jpg"`
		} `json:"images"`
		Episodes *int `json:"episodes"`
	} `json:"data"`
}

func NewClient(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 6 * time.Second}
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.tenrai.org/v1"
	}
	fallbackURL := "https://api.jikan.moe/v4"
	if strings.EqualFold(strings.TrimRight(baseURL, "/"), fallbackURL) {
		fallbackURL = ""
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		FallbackURL: fallbackURL,
		HTTPClient:  httpClient,
	}
}

func ParseURL(raw string) (int, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "myanimelist.net" && parsed.Host != "www.myanimelist.net" {
		return 0, fmt.Errorf("informe uma URL HTTPS válida do MyAnimeList")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "anime" || parts[1] == "" {
		return 0, fmt.Errorf("a URL deve seguir o formato https://myanimelist.net/anime/ID/titulo")
	}
	id, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("o ID do anime no MyAnimeList é inválido")
	}
	return int(id), nil
}

func waitRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt+1) * 350 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) FetchAnime(ctx context.Context, malID int) (Anime, error) {
	if malID <= 0 {
		return Anime{}, fmt.Errorf("ID do MyAnimeList inválido")
	}
	bases := []string{c.BaseURL}
	if c.FallbackURL != "" && !strings.EqualFold(c.BaseURL, c.FallbackURL) {
		bases = append(bases, c.FallbackURL)
	}
	var lastErr error
	for _, baseURL := range bases {
		anime, err := c.fetchFrom(ctx, baseURL, malID)
		if err == nil {
			return anime, nil
		}
		lastErr = err
	}
	return Anime{}, fmt.Errorf("não foi possível consultar os metadados do anime: %w", lastErr)
}

func (c *Client) fetchFrom(ctx context.Context, baseURL string, malID int) (Anime, error) {
	var payload response
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/anime/%d", strings.TrimRight(baseURL, "/"), malID), nil)
		if err != nil {
			return Anime{}, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "subs-catalog/1.0")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if attempt == 1 {
				return Anime{}, fmt.Errorf("consulta falhou: %w", err)
			}
			if err := waitRetry(ctx, attempt); err != nil {
				return Anime{}, err
			}
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			status := resp.StatusCode
			_ = resp.Body.Close()
			if (status == http.StatusTooManyRequests || status >= 500) && attempt < 1 {
				if err := waitRetry(ctx, attempt); err != nil {
					return Anime{}, err
				}
				continue
			}
			return Anime{}, fmt.Errorf("a fonte respondeu HTTP %d", status)
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload)
		_ = resp.Body.Close()
		if err != nil {
			return Anime{}, fmt.Errorf("resposta inválida: %w", err)
		}
		break
	}

	if payload.Data.MALID != malID || strings.TrimSpace(payload.Data.Title) == "" {
		return Anime{}, fmt.Errorf("o anime informado não foi encontrado no MyAnimeList")
	}
	imageURL := payload.Data.Images.JPG.Large
	if imageURL == "" {
		imageURL = payload.Data.Images.JPG.Image
	}
	if imageURL != "" {
		image, err := url.Parse(imageURL)
		if err != nil || image.Scheme != "https" || image.Host != "cdn.myanimelist.net" {
			imageURL = ""
		}
	}
	malURL := payload.Data.URL
	if parsed, err := url.Parse(malURL); err != nil || parsed.Scheme != "https" || parsed.Host != "myanimelist.net" && parsed.Host != "www.myanimelist.net" {
		malURL = fmt.Sprintf("https://myanimelist.net/anime/%d", malID)
	}
	episodes := 0
	if payload.Data.Episodes != nil && *payload.Data.Episodes > 0 {
		episodes = *payload.Data.Episodes
	}
	return Anime{MALID: malID, MALURL: malURL, Title: strings.TrimSpace(payload.Data.Title), ImageURL: imageURL, Episodes: episodes}, nil
}
