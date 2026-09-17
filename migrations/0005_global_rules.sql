-- +goose Up

-- Rules become global: every group shares one rule set. The sentinel chat row
-- (-1, "全局规则") owns the merged rows so the existing moderation_rules
-- table and audit matched_rule_ids keep working unchanged (rule ids stay
-- stable inside the kept rows). Duplicate patterns across the old per-chat
-- rows are deduplicated, keeping the earliest row.

INSERT INTO chat_groups (chat_id, title)
VALUES (-1, '全局规则')
ON CONFLICT (chat_id) DO NOTHING;

-- Keep the earliest row per identical pattern, drop the later duplicates.
WITH dup AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY pattern ORDER BY id) AS rn
    FROM moderation_rules
)
DELETE FROM moderation_rules
WHERE id IN (SELECT id FROM dup WHERE rn > 1);

-- Re-home the remaining rules onto the global sentinel chat.
UPDATE moderation_rules SET chat_id = -1 WHERE chat_id <> -1;

-- +goose Down

-- Merged per-chat scoping cannot be reconstructed exactly; the Down migration
-- only drops the sentinel row. Rules remain on chat_id = -1.
DELETE FROM chat_groups WHERE chat_id = -1;