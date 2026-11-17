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
    uploaded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY uploads_uploaded_at (uploaded_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS upload_files (
    upload_id VARCHAR(32) CHARACTER SET ascii NOT NULL,
    file_id CHAR(64) CHARACTER SET ascii NOT NULL,
    PRIMARY KEY (upload_id, file_id),
    KEY upload_files_file_id (file_id),
    CONSTRAINT upload_files_upload FOREIGN KEY (upload_id) REFERENCES uploads (id) ON DELETE CASCADE,
    CONSTRAINT upload_files_file FOREIGN KEY (file_id) REFERENCES files (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
