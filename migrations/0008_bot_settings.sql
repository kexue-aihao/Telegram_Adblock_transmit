-- +goose Up

-- Runtime switches the operator changes from the panel or from chat: the
-- optional profile (bio) check, the cross-group management permission and the
-- bot owner list. The single row is identified by id = 1; while it is absent
-- the environment defaults (BIO_CHECK_ENABLED, BOT_OWNER_IDS) stay in effect,
-- so upgrades need no new required variables.
CREATE TABLE IF NOT EXISTS bot_settings (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    bio_check_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    cross_group_management BOOLEAN NOT NULL DEFAULT FALSE,
    owner_user_ids BIGINT[] NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down

DROP TABLE IF EXISTS bot_settings;
