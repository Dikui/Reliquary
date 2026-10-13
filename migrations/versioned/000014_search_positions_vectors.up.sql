ALTER TABLE embedding_rebuild_runs DROP CONSTRAINT embedding_rebuild_runs_status_check;
ALTER TABLE embedding_rebuild_runs ADD CONSTRAINT embedding_rebuild_runs_status_check CHECK (status IN ('pending','running','paused','completed','completed_with_errors','failed','cancelled'));
ALTER TABLE chunks ADD COLUMN source_locations jsonb NOT NULL DEFAULT '[]';
ALTER TABLE embedding_rebuild_runs ADD COLUMN candidate boolean NOT NULL DEFAULT false;
ALTER TABLE embedding_rebuild_runs ADD COLUMN switched_at timestamptz;
CREATE TABLE embedding_rebuild_vectors (
 run_id text NOT NULL REFERENCES embedding_rebuild_runs(id) ON DELETE CASCADE,
 kind text NOT NULL, source_id text NOT NULL, attachment_path text NOT NULL DEFAULT '',
 entry_id text NOT NULL DEFAULT '', library_id text NOT NULL DEFAULT '', model_id text NOT NULL,
 dimension integer NOT NULL, embedding halfvec NOT NULL, content_hash text NOT NULL,
 PRIMARY KEY(run_id,kind,source_id,attachment_path)
);
CREATE TABLE embedding_vector_archives (
 run_id text PRIMARY KEY REFERENCES embedding_rebuild_runs(id) ON DELETE CASCADE,
 space_id bigint NOT NULL, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO models(id,space_id,name,type,source,description,parameters,is_default,status,is_builtin)
SELECT 'builtin-qwen3-embedding-06b',1,'qwen3-embedding:0.6b','Embedding','local','Local text embedding',
 '{"embedding_parameters":{"dimension":1024,"truncate_prompt_tokens":8192},"base_url":"http://127.0.0.1:11434"}',false,'active',true WHERE EXISTS (SELECT 1 FROM spaces WHERE id=1)
ON CONFLICT(id) DO NOTHING;
UPDATE spaces SET embedding_model_id='builtin-qwen3-embedding-06b' WHERE embedding_model_id='';
ALTER TABLE spaces ALTER COLUMN embedding_model_id SET DEFAULT 'builtin-qwen3-embedding-06b';
