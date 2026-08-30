-- +goose Up

-- Telegram chat metadata. A row is created lazily when the first rule is
-- added or the first matching message is audited.
CREATE TABLE IF NOT EXISTS chat_groups (
    chat_id BIGINT PRIMARY KEY,
    title VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS moderation_rules (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES chat_groups(chat_id) ON DELETE CASCADE,
    pattern VARCHAR(512) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT moderation_rules_pattern_length CHECK (char_length(pattern) BETWEEN 1 AND 512)
);

CREATE INDEX IF NOT EXISTS ix_moderation_rules_chat_id
    ON moderation_rules (chat_id);

CREATE TABLE IF NOT EXISTS moderation_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL REFERENCES chat_groups(chat_id) ON DELETE CASCADE,
    message_thread_id BIGINT,
    user_id BIGINT,
    message_id BIGINT NOT NULL,
    matched_rule_ids BIGINT[] NOT NULL DEFAULT '{}',
    content_sha256 CHAR(64) NOT NULL,
    content_summary VARCHAR(120) NOT NULL DEFAULT '',
    delete_succeeded BOOLEAN NOT NULL,
    deletion_error TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS ix_moderation_audit_logs_chat_id
    ON moderation_audit_logs (chat_id);
CREATE INDEX IF NOT EXISTS ix_moderation_audit_logs_occurred_at
    ON moderation_audit_logs (occurred_at);

-- +goose Down

DROP TABLE IF EXISTS moderation_audit_logs;
DROP TABLE IF EXISTS moderation_rules;
DROP TABLE IF EXISTS chat_groups;
