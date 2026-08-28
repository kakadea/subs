package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr                string
	AppEnv              string
	BaseURL             string
	CookieSecure        bool
	DatabaseDSN         string
	StorageRoot         string
	MaxUploadBytes      int64
	MaxUploadFiles      int
	MaxUploadBatchBytes int64
	SessionTTL          time.Duration
	DownloadLinkTTL     time.Duration
	SessionCookieName   string
	SessionSecret       string
}

func Load() (Config, error) {
	maxUploadMB, err := intEnv("MAX_UPLOAD_MB", 25)
	if err != nil {
		return Config{}, fmt.Errorf("MAX_UPLOAD_MB: %w", err)
	}
	maxUploadFiles, err := intEnv("MAX_UPLOAD_FILES", 20)
	if err != nil {
		return Config{}, fmt.Errorf("MAX_UPLOAD_FILES: %w", err)
	}
	maxUploadBatchMB, err := intEnv("MAX_UPLOAD_BATCH_MB", 100)
	if err != nil {
		return Config{}, fmt.Errorf("MAX_UPLOAD_BATCH_MB: %w", err)
	}
	sessionHours, err := intEnv("SESSION_TTL_HOURS", 168)
	if err != nil {
		return Config{}, fmt.Errorf("SESSION_TTL_HOURS: %w", err)
	}
	linkHours, err := intEnv("DOWNLOAD_LINK_TTL_HOURS", 24)
	if err != nil {
		return Config{}, fmt.Errorf("DOWNLOAD_LINK_TTL_HOURS: %w", err)
	}

	cfg := Config{
		Addr:                env("ADDR", ":8080"),
		AppEnv:              env("APP_ENV", "production"),
		BaseURL:             strings.TrimRight(env("BASE_URL", "http://localhost:18180"), "/"),
		CookieSecure:        boolEnv("COOKIE_SECURE", true),
		DatabaseDSN:         env("DATABASE_DSN", "subs:subs@tcp(mariadb:3306)/subs?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"),
		StorageRoot:         env("STORAGE_ROOT", "/data"),
		MaxUploadBytes:      int64(maxUploadMB) * 1024 * 1024,
		MaxUploadFiles:      maxUploadFiles,
		MaxUploadBatchBytes: int64(maxUploadBatchMB) * 1024 * 1024,
		SessionTTL:          time.Duration(sessionHours) * time.Hour,
		DownloadLinkTTL:     time.Duration(linkHours) * time.Hour,
		SessionCookieName:   env("SESSION_COOKIE_NAME", "subs_session"),
		SessionSecret:       os.Getenv("SESSION_SECRET"),
	}
	if len(cfg.SessionSecret) < 32 {
		return Config{}, fmt.Errorf("SESSION_SECRET must contain at least 32 characters")
	}
	if cfg.MaxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_UPLOAD_MB must be greater than zero")
	}
	if cfg.MaxUploadBatchBytes < cfg.MaxUploadBytes {
		return Config{}, fmt.Errorf("MAX_UPLOAD_BATCH_MB must be at least MAX_UPLOAD_MB")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	value := env(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes"
}
