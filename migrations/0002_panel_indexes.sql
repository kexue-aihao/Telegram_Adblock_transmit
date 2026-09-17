-- +goose Up

-- The WebUI panel filters audit logs by chat + time range and by matched rule
-- id; the existing single-column indexes cannot serve those lookups.
CREATE INDEX IF NOT EXISTS ix_moderation_audit_logs_chat_time
    ON moderation_audit_logs (chat_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS ix_moderation_audit_logs_matched_rule_ids
    ON moderation_audit_logs USING GIN (matched_rule_ids);

-- +goose Down

DROP INDEX IF EXISTS ix_moderation_audit_logs_matched_rule_ids;
DROP INDEX IF EXISTS ix_moderation_audit_logs_chat_time;