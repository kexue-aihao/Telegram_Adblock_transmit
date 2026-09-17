-- +goose Up

-- Built-in ad filter hits (stable hit ids such as ad_bot_mention) recorded
-- alongside the machine rules that fired.
ALTER TABLE moderation_audit_logs
    ADD COLUMN IF NOT EXISTS builtin_hits TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE moderation_audit_logs
    DROP COLUMN IF EXISTS builtin_hits;