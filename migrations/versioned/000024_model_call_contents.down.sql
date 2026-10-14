DROP TRIGGER entry_model_call_contents_delete ON entries;
DROP FUNCTION delete_entry_model_call_contents();
DROP TABLE model_call_content_deletions;
DROP TABLE model_call_contents;
DROP INDEX model_calls_entry_attempt;
ALTER TABLE model_calls DROP COLUMN span_id;
ALTER TABLE model_calls DROP COLUMN processing_attempt;
