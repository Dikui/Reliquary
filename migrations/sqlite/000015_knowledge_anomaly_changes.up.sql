ALTER TABLE knowledge_anomalies ADD COLUMN proposed_remove_tag_ids TEXT NOT NULL DEFAULT '[]';
ALTER TABLE knowledge_anomalies ADD COLUMN applied_changes TEXT NOT NULL DEFAULT '{}';
