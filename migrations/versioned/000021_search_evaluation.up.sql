CREATE TABLE search_records (
 id text PRIMARY KEY, space_id bigint NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(), source text NOT NULL CHECK(source IN ('web','http','mcp','evaluation')),
 method text NOT NULL CHECK(method IN ('ordinary','graph')), query text NOT NULL,
 libraries jsonb NOT NULL DEFAULT '[]', request jsonb NOT NULL DEFAULT '{}', results jsonb NOT NULL DEFAULT '[]',
 duration_ms bigint NOT NULL DEFAULT 0, fallback_fulltext boolean NOT NULL DEFAULT false,
 reranked boolean NOT NULL DEFAULT false, evaluation_run_id text NOT NULL DEFAULT '', error text NOT NULL DEFAULT ''
);
CREATE INDEX search_records_scope_time ON search_records(space_id,created_at DESC,id);
CREATE INDEX search_records_source ON search_records(space_id,source,created_at DESC);
CREATE INDEX search_records_libraries ON search_records USING gin(libraries);
CREATE TABLE evaluation_sets (
 id text PRIMARY KEY, space_id bigint NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
 name text NOT NULL CHECK(length(trim(name))>0), description text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE evaluation_questions (
 id text PRIMARY KEY, space_id bigint NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
 set_id text NOT NULL REFERENCES evaluation_sets(id) ON DELETE CASCADE,
 method text NOT NULL CHECK(method IN ('ordinary','graph')), request jsonb NOT NULL,
 source text NOT NULL CHECK(source IN ('manual','search_record','ai')),
 status text NOT NULL CHECK(status IN ('pending','confirmed')), source_record_id text NOT NULL DEFAULT '',
 suggested_results jsonb NOT NULL DEFAULT '[]', answer_deleted boolean NOT NULL DEFAULT false,
 revision integer NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX evaluation_questions_set ON evaluation_questions(space_id,set_id,created_at,id);
CREATE TABLE evaluation_answers (
 question_id text NOT NULL REFERENCES evaluation_questions(id) ON DELETE CASCADE,
 entry_id text NOT NULL, grade integer NOT NULL DEFAULT 1 CHECK(grade BETWEEN 0 AND 10),
 PRIMARY KEY(question_id,entry_id)
);
CREATE INDEX evaluation_answers_entry ON evaluation_answers(entry_id);
CREATE TABLE evaluation_runs (
 id text PRIMARY KEY, space_id bigint NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
 set_id text NOT NULL, set_snapshot jsonb NOT NULL, options jsonb NOT NULL, snapshot jsonb NOT NULL DEFAULT '{}',
 status text NOT NULL CHECK(status IN ('running','completed','failed')), summary jsonb NOT NULL DEFAULT '{}',
 error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz
);
CREATE INDEX evaluation_runs_set ON evaluation_runs(space_id,set_id,created_at DESC,id);
CREATE TABLE evaluation_run_results (
 run_id text NOT NULL REFERENCES evaluation_runs(id) ON DELETE CASCADE, question_id text NOT NULL,
 question jsonb NOT NULL, snapshot jsonb NOT NULL, results jsonb NOT NULL, metrics jsonb NOT NULL,
 hits jsonb NOT NULL DEFAULT '[]', error text NOT NULL DEFAULT '', PRIMARY KEY(run_id,question_id)
);
CREATE FUNCTION mark_evaluation_answer_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'DELETE' OR (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) THEN
  UPDATE evaluation_questions SET answer_deleted=true,updated_at=now()
   WHERE id IN (SELECT question_id FROM evaluation_answers WHERE entry_id=OLD.id);
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER evaluation_answer_deleted AFTER DELETE OR UPDATE OF deleted_at ON entries
 FOR EACH ROW EXECUTE FUNCTION mark_evaluation_answer_deleted();
