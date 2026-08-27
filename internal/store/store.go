package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("not found")

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(254) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    role ENUM('admin','viewer') NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS sessions (
    token_hash CHAR(64) NOT NULL PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    INDEX idx_sessions_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS subtitles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    public_id CHAR(32) NOT NULL UNIQUE,
    title VARCHAR(255) NOT NULL,
    episode VARCHAR(64) NOT NULL DEFAULT '',
    season VARCHAR(64) NOT NULL DEFAULT '',
    language VARCHAR(64) NOT NULL DEFAULT 'Português',
    format VARCHAR(8) NOT NULL,
    original_filename VARCHAR(255) NOT NULL,
    storage_name CHAR(68) NOT NULL UNIQUE,
    storage_path VARCHAR(512) NOT NULL,
    file_size BIGINT UNSIGNED NOT NULL,
    checksum CHAR(64) NOT NULL,
    version VARCHAR(64) NOT NULL DEFAULT '1.0',
    visibility ENUM('public','private') NOT NULL DEFAULT 'public',
    created_by BIGINT UNSIGNED NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    deleted_at DATETIME NULL,
    CONSTRAINT fk_subtitles_user FOREIGN KEY (created_by) REFERENCES users(id),
    INDEX idx_subtitles_search (title, episode, language),
    INDEX idx_subtitles_public (public_id, visibility, deleted_at),
    INDEX idx_subtitles_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS download_links (
    token_hash CHAR(64) NOT NULL PRIMARY KEY,
    subtitle_id BIGINT UNSIGNED NOT NULL,
    expires_at DATETIME NOT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_links_subtitle FOREIGN KEY (subtitle_id) REFERENCES subtitles(id) ON DELETE CASCADE,
    CONSTRAINT fk_links_user FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE,
    INDEX idx_links_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NULL,
    action VARCHAR(64) NOT NULL,
    subtitle_id BIGINT UNSIGNED NULL,
    ip_address VARCHAR(45) NOT NULL DEFAULT '',
    metadata JSON NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_audit_created (created_at),
    CONSTRAINT fk_audit_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT fk_audit_subtitle FOREIGN KEY (subtitle_id) REFERENCES subtitles(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

type User struct {
	ID           uint64
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

func (u User) IsAdmin() bool { return u.Role == "admin" }

type Subtitle struct {
	ID               uint64
	PublicID         string
	Title            string
	Episode          string
	Season           string
	Language         string
	Format           string
	OriginalFilename string
	StorageName      string
	StoragePath      string
	FileSize         int64
	Checksum         string
	Version          string
	Visibility       string
	CreatedBy        uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type DownloadLink struct {
	SubtitleID uint64
	ExpiresAt  time.Time
}

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

func (s *Store) Migrate(ctx context.Context) error {
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("schema migration: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureAdmin(ctx context.Context, email, password string) error {
	if strings.TrimSpace(email) == "" || len(password) < 12 {
		return fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD with at least 12 characters are required")
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO users (email, password_hash, role) VALUES (?, ?, 'admin')`, strings.ToLower(strings.TrimSpace(email)), string(hash))
	return err
}

func (s *Store) Authenticate(ctx context.Context, email, password string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id, email, password_hash, role, created_at FROM users WHERE email = ?`, strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, ErrNotFound
	}
	return u, nil
}

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	// Tokens are random bearer credentials; only their SHA-256 digest is stored.
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (s *Store) CreateSession(ctx context.Context, userID uint64, expiresAt time.Time) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, hashToken(token), userID, expiresAt)
	return token, err
}

func (s *Store) GetSessionUser(ctx context.Context, token string) (User, error) {
	if len(token) < 32 {
		return User{}, ErrNotFound
	}
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT u.id, u.email, u.password_hash, u.role, u.created_at FROM sessions x JOIN users u ON u.id = x.user_id WHERE x.token_hash = ? AND x.expires_at > UTC_TIMESTAMP()`, hashToken(token)).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

func (s *Store) CreateSubtitle(ctx context.Context, sub Subtitle) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO subtitles (public_id, title, episode, season, language, format, original_filename, storage_name, storage_path, file_size, checksum, version, visibility, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, sub.PublicID, sub.Title, sub.Episode, sub.Season, sub.Language, sub.Format, sub.OriginalFilename, sub.StorageName, sub.StoragePath, sub.FileSize, sub.Checksum, sub.Version, sub.Visibility, sub.CreatedBy)
	return err
}

func (s *Store) ListSubtitles(ctx context.Context, query string, includePrivate bool) ([]Subtitle, error) {
	pattern := "%" + strings.ReplaceAll(strings.TrimSpace(query), "%", "\\%") + "%"
	visibility := "AND visibility = 'public'"
	if includePrivate {
		visibility = ""
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, public_id, title, episode, season, language, format, original_filename, storage_name, storage_path, file_size, checksum, version, visibility, created_by, created_at, updated_at FROM subtitles WHERE deleted_at IS NULL `+visibility+` AND (title LIKE ? OR episode LIKE ? OR language LIKE ?) ORDER BY created_at DESC LIMIT 200`, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Subtitle
	for rows.Next() {
		var sub Subtitle
		if err := rows.Scan(&sub.ID, &sub.PublicID, &sub.Title, &sub.Episode, &sub.Season, &sub.Language, &sub.Format, &sub.OriginalFilename, &sub.StorageName, &sub.StoragePath, &sub.FileSize, &sub.Checksum, &sub.Version, &sub.Visibility, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	return result, rows.Err()
}

func (s *Store) GetSubtitle(ctx context.Context, publicID string, includePrivate bool) (Subtitle, error) {
	visibility := "AND visibility = 'public'"
	if includePrivate {
		visibility = ""
	}
	var sub Subtitle
	err := s.DB.QueryRowContext(ctx, `SELECT id, public_id, title, episode, season, language, format, original_filename, storage_name, storage_path, file_size, checksum, version, visibility, created_by, created_at, updated_at FROM subtitles WHERE public_id = ? AND deleted_at IS NULL `+visibility, publicID).Scan(&sub.ID, &sub.PublicID, &sub.Title, &sub.Episode, &sub.Season, &sub.Language, &sub.Format, &sub.OriginalFilename, &sub.StorageName, &sub.StoragePath, &sub.FileSize, &sub.Checksum, &sub.Version, &sub.Visibility, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Subtitle{}, ErrNotFound
	}
	return sub, err
}

func (s *Store) GetSubtitleByID(ctx context.Context, id uint64) (Subtitle, error) {
	var sub Subtitle
	err := s.DB.QueryRowContext(ctx, `SELECT id, public_id, title, episode, season, language, format, original_filename, storage_name, storage_path, file_size, checksum, version, visibility, created_by, created_at, updated_at FROM subtitles WHERE id = ? AND deleted_at IS NULL`, id).Scan(&sub.ID, &sub.PublicID, &sub.Title, &sub.Episode, &sub.Season, &sub.Language, &sub.Format, &sub.OriginalFilename, &sub.StorageName, &sub.StoragePath, &sub.FileSize, &sub.Checksum, &sub.Version, &sub.Visibility, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Subtitle{}, ErrNotFound
	}
	return sub, err
}

func (s *Store) DeleteSubtitle(ctx context.Context, publicID string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE subtitles SET deleted_at = UTC_TIMESTAMP() WHERE public_id = ? AND deleted_at IS NULL`, publicID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateDownloadLink(ctx context.Context, subtitleID, userID uint64, expiresAt time.Time) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO download_links (token_hash, subtitle_id, expires_at, created_by) VALUES (?, ?, ?, ?)`, hashToken(token), subtitleID, expiresAt, userID)
	return token, err
}

func (s *Store) GetDownloadLink(ctx context.Context, token string) (DownloadLink, error) {
	if len(token) < 32 {
		return DownloadLink{}, ErrNotFound
	}
	var link DownloadLink
	err := s.DB.QueryRowContext(ctx, `SELECT subtitle_id, expires_at FROM download_links WHERE token_hash = ? AND expires_at > UTC_TIMESTAMP()`, hashToken(token)).Scan(&link.SubtitleID, &link.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DownloadLink{}, ErrNotFound
	}
	return link, err
}

func (s *Store) Audit(ctx context.Context, userID *uint64, action string, subtitleID *uint64, ip, metadata string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO audit_logs (user_id, action, subtitle_id, ip_address, metadata) VALUES (?, ?, ?, ?, NULLIF(?, ''))`, userID, action, subtitleID, ip, metadata)
	return err
}
