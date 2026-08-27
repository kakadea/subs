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
