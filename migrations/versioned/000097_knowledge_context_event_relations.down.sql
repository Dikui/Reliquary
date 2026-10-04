DROP INDEX IF EXISTS idx_kcee_related_event;
ALTER TABLE knowledge_context_facts DROP COLUMN original_predicate;
ALTER TABLE knowledge_context_evidence DROP COLUMN source_kind;
ALTER TABLE knowledge_context_event_evidence DROP COLUMN related_event_id;
ALTER TABLE knowledge_context_event_evidence DROP COLUMN relation;
