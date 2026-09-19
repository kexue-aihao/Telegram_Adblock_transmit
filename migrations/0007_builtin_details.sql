-- +goose Up

-- Additive and nullable so old audit rows and older application versions keep
-- working. Only library metadata and short evidence labels are stored here.
ALTER TABLE moderation_audit_logs
    ADD COLUMN IF NOT EXISTS builtin_details JSONB;

CREATE INDEX IF NOT EXISTS ix_moderation_audit_logs_strikes
    ON moderation_audit_logs (chat_id, user_id, occurred_at, message_id);

-- +goose Down

DROP INDEX IF EXISTS ix_moderation_audit_logs_strikes;
ALTER TABLE moderation_audit_logs
    DROP COLUMN IF EXISTS builtin_details;
