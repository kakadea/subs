package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

CREATE TABLE IF NOT EXISTS anime_projects (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    public_id CHAR(32) NOT NULL UNIQUE,
    mal_id INT UNSIGNED NOT NULL UNIQUE,
    mal_url VARCHAR(512) NOT NULL,
    title VARCHAR(255) NOT NULL,
    image_url VARCHAR(512) NOT NULL DEFAULT '',
    episodes INT UNSIGNED NOT NULL DEFAULT 0,
    visibility ENUM('public','private') NOT NULL DEFAULT 'private',
    created_by BIGINT UNSIGNED NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_projects_user FOREIGN KEY (created_by) REFERENCES users(id),
    INDEX idx_projects_title (title),
    INDEX idx_projects_visibility (visibility, updated_at),
    INDEX idx_projects_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

	CREATE TABLE IF NOT EXISTS project_sources (
	    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	    project_id BIGINT UNSIGNED NOT NULL,
	    public_id CHAR(32) NOT NULL UNIQUE,
	    name VARCHAR(160) NOT NULL,
	    url VARCHAR(1024) NOT NULL,
	    description VARCHAR(500) NOT NULL DEFAULT '',
	    created_by BIGINT UNSIGNED NOT NULL,
	    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	    CONSTRAINT fk_sources_project FOREIGN KEY (project_id) REFERENCES anime_projects(id) ON DELETE CASCADE,
	    CONSTRAINT fk_sources_user FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE,
	    INDEX idx_sources_project (project_id, created_at)
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
    project_id BIGINT UNSIGNED NULL,
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
    CONSTRAINT fk_subtitles_user FOREIGN KEY (created_by) REFERENCES users(id),
    CONSTRAINT fk_subtitles_project FOREIGN KEY (project_id) REFERENCES anime_projects(id) ON DELETE SET NULL,
    INDEX idx_subtitles_search (title, episode, language),
    INDEX idx_subtitles_project (project_id),
    INDEX idx_subtitles_public (public_id, visibility),
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

type AnimeProject struct {
	ID            uint64
	PublicID      string
	MALID         int
	MALURL        string
	Title         string
	ImageURL      string
	Episodes      int
	Visibility    string
	SubtitleCount int
	CreatedBy     uint64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ProjectSource struct {
	ID          uint64
	ProjectID   uint64
	PublicID    string
	Name        string
	URL         string
	Description string
	CreatedBy   uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Subtitle struct {
	ID               uint64
	ProjectID        *uint64
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

func (s *Store) Migrate(ctx context.Context, storageRoots ...string) error {
	storageRoot := ""
	if len(storageRoots) > 0 {
		storageRoot = filepath.Clean(storageRoots[0])
	}
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("schema migration: %w", err)
		}
	}
	var projectColumn int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subtitles' AND COLUMN_NAME = 'project_id'`).Scan(&projectColumn); err != nil {
		return fmt.Errorf("check project migration: %w", err)
	}
	if projectColumn == 0 {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE subtitles ADD COLUMN project_id BIGINT UNSIGNED NULL AFTER id, ADD INDEX idx_subtitles_project (project_id), ADD CONSTRAINT fk_subtitles_project FOREIGN KEY (project_id) REFERENCES anime_projects(id) ON DELETE SET NULL`); err != nil {
			return fmt.Errorf("add project relation: %w", err)
		}
	}
	var projectVisibilityColumn int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'anime_projects' AND COLUMN_NAME = 'visibility'`).Scan(&projectVisibilityColumn); err != nil {
		return fmt.Errorf("check project visibility migration: %w", err)
	}
	if projectVisibilityColumn == 0 {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE anime_projects ADD COLUMN visibility ENUM('public','private') NOT NULL DEFAULT 'private' AFTER episodes, ADD INDEX idx_projects_visibility (visibility, updated_at)`); err != nil {
			return fmt.Errorf("add project visibility: %w", err)
		}
	}
	var deletedAtColumn int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subtitles' AND COLUMN_NAME = 'deleted_at'`).Scan(&deletedAtColumn); err != nil {
		return fmt.Errorf("check deleted subtitle migration: %w", err)
	}
	if deletedAtColumn > 0 {
		var legacyPaths []string
		rows, err := s.DB.QueryContext(ctx, `SELECT storage_path FROM subtitles WHERE deleted_at IS NOT NULL`)
		if err != nil {
			return fmt.Errorf("list logically deleted subtitles: %w", err)
		}
		for rows.Next() {
			var storagePath string
			if err := rows.Scan(&storagePath); err != nil {
				rows.Close()
				return fmt.Errorf("read deleted subtitle path: %w", err)
			}
			legacyPaths = append(legacyPaths, storagePath)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("list deleted subtitle paths: %w", err)
		}
		rows.Close()
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM subtitles WHERE deleted_at IS NOT NULL`); err != nil {
			return fmt.Errorf("purge logically deleted subtitles: %w", err)
		}
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE subtitles DROP INDEX idx_subtitles_public, DROP COLUMN deleted_at, ADD INDEX idx_subtitles_public (public_id, visibility)`); err != nil {
			return fmt.Errorf("remove logical deletion: %w", err)
		}
		if storageRoot != "" {
			for _, relativePath := range legacyPaths {
				filePath := filepath.Join(storageRoot, filepath.Clean(filepath.FromSlash(relativePath)))
				if filePath == storageRoot || !strings.HasPrefix(filePath, storageRoot+string(os.PathSeparator)) {
					return fmt.Errorf("invalid legacy storage path")
				}
				if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("remove legacy subtitle file: %w", err)
				}
			}
		}
	}
	return nil
}

func (s *Store) EnsureAdmin(ctx context.Context, email, password string) error {
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if strings.TrimSpace(email) == "" || len(password) < 12 {
		return fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD with at least 12 characters are required for the first admin")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO users (email, password_hash, role) VALUES (?, ?, 'admin')`, strings.ToLower(strings.TrimSpace(email)), string(hash))
	return err
}

func (s *Store) SetProjectVisibility(ctx context.Context, publicID, visibility string) error {
	if visibility != "public" && visibility != "private" {
		return fmt.Errorf("invalid project visibility")
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE anime_projects SET visibility = ? WHERE public_id = ?`, visibility, publicID)
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

func (s *Store) SetAdminPassword(ctx context.Context, email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(password) < 12 {
		return fmt.Errorf("admin email is required and password must contain at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID uint64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ? AND role = 'admin' FOR UPDATE`, email).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("admin user not found: %s", email)
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
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

const subtitleColumns = `id, project_id, public_id, title, episode, season, language, format, original_filename, storage_name, storage_path, file_size, checksum, version, visibility, created_by, created_at, updated_at`
const subtitleColumnsQualified = `s.id, s.project_id, s.public_id, s.title, s.episode, s.season, s.language, s.format, s.original_filename, s.storage_name, s.storage_path, s.file_size, s.checksum, s.version, s.visibility, s.created_by, s.created_at, s.updated_at`

func (s *Store) CreateProject(ctx context.Context, project AnimeProject) error {
	visibility := project.Visibility
	if visibility != "public" {
		visibility = "private"
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO anime_projects (public_id, mal_id, mal_url, title, image_url, episodes, visibility, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, project.PublicID, project.MALID, project.MALURL, project.Title, project.ImageURL, project.Episodes, visibility, project.CreatedBy)
	return err
}

func (s *Store) GetProjectByMALID(ctx context.Context, malID int) (AnimeProject, error) {
	var project AnimeProject
	err := s.DB.QueryRowContext(ctx, `SELECT id, public_id, mal_id, mal_url, title, image_url, episodes, visibility, created_by, created_at, updated_at FROM anime_projects WHERE mal_id = ?`, malID).Scan(&project.ID, &project.PublicID, &project.MALID, &project.MALURL, &project.Title, &project.ImageURL, &project.Episodes, &project.Visibility, &project.CreatedBy, &project.CreatedAt, &project.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AnimeProject{}, ErrNotFound
	}
	return project, err
}

func (s *Store) ListProjects(ctx context.Context, query string, includePrivate bool) ([]AnimeProject, error) {
	pattern := "%" + strings.ReplaceAll(strings.TrimSpace(query), "%", "\\%") + "%"
	subtitleVisibility := "AND s.visibility = 'public'"
	projectWhere := "WHERE p.visibility = 'public' AND (p.title LIKE ? OR p.mal_url LIKE ?)"
	if includePrivate {
		subtitleVisibility = ""
		projectWhere = "WHERE (p.title LIKE ? OR p.mal_url LIKE ?)"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.public_id, p.mal_id, p.mal_url, p.title, p.image_url, p.episodes, p.visibility, p.created_by, p.created_at, p.updated_at, COUNT(s.id) FROM anime_projects p LEFT JOIN subtitles s ON s.project_id = p.id `+subtitleVisibility+` `+projectWhere+` GROUP BY p.id ORDER BY p.updated_at DESC, p.created_at DESC LIMIT 200`, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AnimeProject
	for rows.Next() {
		var project AnimeProject
		if err := rows.Scan(&project.ID, &project.PublicID, &project.MALID, &project.MALURL, &project.Title, &project.ImageURL, &project.Episodes, &project.Visibility, &project.CreatedBy, &project.CreatedAt, &project.UpdatedAt, &project.SubtitleCount); err != nil {
			return nil, err
		}
		result = append(result, project)
	}
	return result, rows.Err()
}

func (s *Store) GetProjectByID(ctx context.Context, id uint64) (AnimeProject, error) {
	var project AnimeProject
	err := s.DB.QueryRowContext(ctx, `SELECT id, public_id, mal_id, mal_url, title, image_url, episodes, visibility, created_by, created_at, updated_at FROM anime_projects WHERE id = ?`, id).Scan(&project.ID, &project.PublicID, &project.MALID, &project.MALURL, &project.Title, &project.ImageURL, &project.Episodes, &project.Visibility, &project.CreatedBy, &project.CreatedAt, &project.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AnimeProject{}, ErrNotFound
	}
	return project, err
}

func (s *Store) GetProject(ctx context.Context, publicID string, includePrivate bool) (AnimeProject, error) {
	subtitleVisibility := "AND s.visibility = 'public'"
	projectVisibility := "AND p.visibility = 'public'"
	if includePrivate {
		subtitleVisibility = ""
		projectVisibility = ""
	}
	var project AnimeProject
	err := s.DB.QueryRowContext(ctx, `SELECT p.id, p.public_id, p.mal_id, p.mal_url, p.title, p.image_url, p.episodes, p.visibility, p.created_by, p.created_at, p.updated_at, COUNT(s.id) FROM anime_projects p LEFT JOIN subtitles s ON s.project_id = p.id `+subtitleVisibility+` WHERE p.public_id = ? `+projectVisibility+` GROUP BY p.id`, publicID).Scan(&project.ID, &project.PublicID, &project.MALID, &project.MALURL, &project.Title, &project.ImageURL, &project.Episodes, &project.Visibility, &project.CreatedBy, &project.CreatedAt, &project.UpdatedAt, &project.SubtitleCount)
	if errors.Is(err, sql.ErrNoRows) {
		return AnimeProject{}, ErrNotFound
	}
	return project, err
}

func (s *Store) ListProjectSources(ctx context.Context, projectID uint64, includePrivate bool) ([]ProjectSource, error) {
	visibility := "JOIN anime_projects p ON p.id = ps.project_id WHERE ps.project_id = ? AND p.visibility = 'public'"
	if includePrivate {
		visibility = "WHERE ps.project_id = ?"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT ps.id, ps.project_id, ps.public_id, ps.name, ps.url, ps.description, ps.created_by, ps.created_at, ps.updated_at FROM project_sources ps `+visibility+` ORDER BY ps.created_at ASC, ps.id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ProjectSource
	for rows.Next() {
		var source ProjectSource
		if err := rows.Scan(&source.ID, &source.ProjectID, &source.PublicID, &source.Name, &source.URL, &source.Description, &source.CreatedBy, &source.CreatedAt, &source.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	return result, rows.Err()
}

func (s *Store) CreateProjectSource(ctx context.Context, source ProjectSource) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO project_sources (project_id, public_id, name, url, description, created_by) VALUES (?, ?, ?, ?, ?, ?)`, source.ProjectID, source.PublicID, source.Name, source.URL, source.Description, source.CreatedBy)
	return err
}

func (s *Store) GetProjectSource(ctx context.Context, publicID string) (ProjectSource, error) {
	var source ProjectSource
	err := s.DB.QueryRowContext(ctx, `SELECT id, project_id, public_id, name, url, description, created_by, created_at, updated_at FROM project_sources WHERE public_id = ?`, publicID).Scan(&source.ID, &source.ProjectID, &source.PublicID, &source.Name, &source.URL, &source.Description, &source.CreatedBy, &source.CreatedAt, &source.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectSource{}, ErrNotFound
	}
	return source, err
}

func (s *Store) DeleteProjectSource(ctx context.Context, publicID string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM project_sources WHERE public_id = ?`, publicID)
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

func (s *Store) ListProjectSubtitles(ctx context.Context, projectID uint64, includePrivate bool) ([]Subtitle, error) {
	visibility := "AND visibility = 'public'"
	if includePrivate {
		visibility = ""
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+subtitleColumns+` FROM subtitles WHERE project_id = ? `+visibility+` ORDER BY CASE WHEN episode = '' THEN 1 ELSE 0 END, CAST(episode AS UNSIGNED), created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Subtitle
	for rows.Next() {
		var sub Subtitle
		if err := rows.Scan(&sub.ID, &sub.ProjectID, &sub.PublicID, &sub.Title, &sub.Episode, &sub.Season, &sub.Language, &sub.Format, &sub.OriginalFilename, &sub.StorageName, &sub.StoragePath, &sub.FileSize, &sub.Checksum, &sub.Version, &sub.Visibility, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	return result, rows.Err()
}

func (s *Store) CreateSubtitle(ctx context.Context, sub Subtitle) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO subtitles (project_id, public_id, title, episode, season, language, format, original_filename, storage_name, storage_path, file_size, checksum, version, visibility, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, sub.ProjectID, sub.PublicID, sub.Title, sub.Episode, sub.Season, sub.Language, sub.Format, sub.OriginalFilename, sub.StorageName, sub.StoragePath, sub.FileSize, sub.Checksum, sub.Version, sub.Visibility, sub.CreatedBy)
	return err
}

func (s *Store) ListSubtitles(ctx context.Context, query string, includePrivate bool) ([]Subtitle, error) {
	pattern := "%" + strings.ReplaceAll(strings.TrimSpace(query), "%", "\\%") + "%"
	visibility := "AND visibility = 'public'"
	if includePrivate {
		visibility = ""
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+subtitleColumns+` FROM subtitles WHERE project_id IS NULL `+visibility+` AND (title LIKE ? OR episode LIKE ? OR language LIKE ?) ORDER BY created_at DESC LIMIT 200`, pattern, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Subtitle
	for rows.Next() {
		var sub Subtitle
		if err := rows.Scan(&sub.ID, &sub.ProjectID, &sub.PublicID, &sub.Title, &sub.Episode, &sub.Season, &sub.Language, &sub.Format, &sub.OriginalFilename, &sub.StorageName, &sub.StoragePath, &sub.FileSize, &sub.Checksum, &sub.Version, &sub.Visibility, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	return result, rows.Err()
}

func (s *Store) GetSubtitle(ctx context.Context, publicID string, includePrivate bool) (Subtitle, error) {
	visibility := "AND s.visibility = 'public' AND p.visibility = 'public'"
	join := "JOIN anime_projects p ON p.id = s.project_id"
	if includePrivate {
		visibility = ""
		join = "LEFT JOIN anime_projects p ON p.id = s.project_id"
	}
	var sub Subtitle
	err := s.DB.QueryRowContext(ctx, `SELECT `+subtitleColumnsQualified+` FROM subtitles s `+join+` WHERE s.public_id = ? `+visibility, publicID).Scan(&sub.ID, &sub.ProjectID, &sub.PublicID, &sub.Title, &sub.Episode, &sub.Season, &sub.Language, &sub.Format, &sub.OriginalFilename, &sub.StorageName, &sub.StoragePath, &sub.FileSize, &sub.Checksum, &sub.Version, &sub.Visibility, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Subtitle{}, ErrNotFound
	}
	return sub, err
}

func (s *Store) GetSubtitleByID(ctx context.Context, id uint64) (Subtitle, error) {
	var sub Subtitle
	err := s.DB.QueryRowContext(ctx, `SELECT `+subtitleColumns+` FROM subtitles WHERE id = ?`, id).Scan(&sub.ID, &sub.ProjectID, &sub.PublicID, &sub.Title, &sub.Episode, &sub.Season, &sub.Language, &sub.Format, &sub.OriginalFilename, &sub.StorageName, &sub.StoragePath, &sub.FileSize, &sub.Checksum, &sub.Version, &sub.Visibility, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Subtitle{}, ErrNotFound
	}
	return sub, err
}

func (s *Store) DeleteSubtitle(ctx context.Context, publicID string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM subtitles WHERE public_id = ?`, publicID)
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
