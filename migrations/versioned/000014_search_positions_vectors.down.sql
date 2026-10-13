ALTER TABLE embedding_rebuild_runs DROP CONSTRAINT embedding_rebuild_runs_status_check;
ALTER TABLE embedding_rebuild_runs ADD CONSTRAINT embedding_rebuild_runs_status_check CHECK (status IN ('pending','running','paused','completed','completed_with_errors','failed'));
ALTER TABLE spaces ALTER COLUMN embedding_model_id SET DEFAULT '';
UPDATE spaces SET embedding_model_id='' WHERE embedding_model_id='builtin-qwen3-embedding-06b';
DELETE FROM models WHERE id='builtin-qwen3-embedding-06b';
DROP TABLE embedding_vector_archives;
DROP TABLE embedding_rebuild_vectors;
ALTER TABLE embedding_rebuild_runs DROP COLUMN switched_at, DROP COLUMN candidate;
ALTER TABLE chunks DROP COLUMN source_locations;
