CREATE TABLE IF NOT EXISTS users (
    id CHAR(32) CHARACTER SET ascii NOT NULL DEFAULT (REPLACE(UUID(), '-', '')),
    username VARCHAR(255) NOT NULL,
    joined_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS roles (
    id VARCHAR(32) CHARACTER SET ascii NOT NULL,
    name VARCHAR(255) NOT NULL,
    moderate_sounds BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO roles (id, name, moderate_sounds) VALUES
    ('user', 'User', FALSE),
    ('moderator', 'Moderator', TRUE),
    ('admin', 'Admin', TRUE);

CREATE TABLE IF NOT EXISTS role_assigns (
    role_id VARCHAR(32) CHARACTER SET ascii NOT NULL,
    user_id CHAR(32) CHARACTER SET ascii NOT NULL,
    assigned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (role_id, user_id),
    KEY role_assigns_user_id (user_id),
    CONSTRAINT role_assigns_role FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE,
    CONSTRAINT role_assigns_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO role_assigns (role_id, user_id)
    SELECT 'user', id FROM users WHERE NOT EXISTS (SELECT 1 FROM role_assigns);

CREATE TABLE IF NOT EXISTS connections (
    user_id CHAR(32) CHARACTER SET ascii NOT NULL,
    platform VARCHAR(32) CHARACTER SET ascii NOT NULL,
    alias_id VARCHAR(255) NOT NULL,
    connected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, platform),
    UNIQUE KEY connections_alias (platform, alias_id),
    CONSTRAINT connections_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS files (
    id CHAR(64) CHARACTER SET ascii NOT NULL,
    mime VARCHAR(255) NOT NULL,
    extension VARCHAR(16) NOT NULL,
    size BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS uploads (
    id VARCHAR(32) CHARACTER SET ascii NOT NULL,
    code VARCHAR(255) NOT NULL,
    user_id CHAR(32) CHARACTER SET ascii NULL,
    visibility TINYINT NOT NULL DEFAULT 1,
    uploaded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY uploads_uploaded_at (uploaded_at, id),
    KEY uploads_visibility (visibility, uploaded_at, id),
    KEY uploads_user_id (user_id),
    CONSTRAINT uploads_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS upload_files (
    upload_id VARCHAR(32) CHARACTER SET ascii NOT NULL,
    file_id CHAR(64) CHARACTER SET ascii NOT NULL,
    PRIMARY KEY (upload_id, file_id),
    KEY upload_files_file_id (file_id),
    CONSTRAINT upload_files_upload FOREIGN KEY (upload_id) REFERENCES uploads (id) ON DELETE CASCADE,
    CONSTRAINT upload_files_file FOREIGN KEY (file_id) REFERENCES files (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS rewards (
    id VARCHAR(32) CHARACTER SET ascii NOT NULL,
    user_id CHAR(32) CHARACTER SET ascii NOT NULL,
    type VARCHAR(32) CHARACTER SET ascii NOT NULL,
    data TEXT NOT NULL,
    data_hash BINARY(32) AS (UNHEX(SHA2(data, 256))) STORED,
    action VARCHAR(32) CHARACTER SET ascii NOT NULL DEFAULT 'sound',
    upload_id VARCHAR(32) CHARACTER SET ascii NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY rewards_unique (user_id, type, data_hash),
    KEY rewards_user_id (user_id, created_at, id),
    KEY rewards_upload_id (upload_id),
    CONSTRAINT rewards_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT rewards_upload FOREIGN KEY (upload_id) REFERENCES uploads (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS twitch_tokens (
    user_id CHAR(32) CHARACTER SET ascii NOT NULL,
    twitch_id VARCHAR(255) NOT NULL,
    access_token VARCHAR(255) CHARACTER SET ascii NOT NULL,
    refresh_token VARCHAR(255) CHARACTER SET ascii NOT NULL,
    scopes VARCHAR(1024) CHARACTER SET ascii NOT NULL,
    expires_at TIMESTAMP NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT twitch_tokens_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

DROP TRIGGER IF EXISTS users_before_delete;

CREATE TRIGGER users_before_delete BEFORE DELETE ON users FOR EACH ROW
BEGIN
    UPDATE uploads SET user_id = NULL WHERE user_id = OLD.id;
    DELETE FROM rewards WHERE user_id = OLD.id;
    DELETE FROM twitch_tokens WHERE user_id = OLD.id;
    DELETE FROM connections WHERE user_id = OLD.id;
    DELETE FROM role_assigns WHERE user_id = OLD.id;
END;
