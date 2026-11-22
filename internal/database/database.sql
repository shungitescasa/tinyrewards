CREATE TABLE IF NOT EXISTS users (
    id CHAR(32) CHARACTER SET ascii NOT NULL DEFAULT (REPLACE(UUID(), '-', '')),
    username VARCHAR(255) NOT NULL,
    joined_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

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
    uploaded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY uploads_uploaded_at (uploaded_at, id),
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
