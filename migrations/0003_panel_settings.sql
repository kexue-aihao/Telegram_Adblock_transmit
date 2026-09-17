-- +goose Up

-- WebUI panel login credentials managed from the settings page. The row is
-- absent on fresh installs, in which case the panel falls back to the
-- WEBUI_USERNAME / WEBUI_PASSWORD environment variables. Only a SHA-256 hex
-- digest of the password is stored; plaintext never reaches the database.
CREATE TABLE IF NOT EXISTS panel_settings (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    username VARCHAR(64) NOT NULL,
    password_hash CHAR(64) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE IF EXISTS panel_settings;