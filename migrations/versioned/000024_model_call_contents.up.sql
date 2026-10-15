ALTER TABLE model_calls ADD COLUMN processing_attempt integer NOT NULL DEFAULT 0 CHECK (processing_attempt >= 0);
ALTER TABLE model_calls ADD COLUMN span_id text NOT NULL DEFAULT '';
CREATE INDEX model_calls_entry_attempt ON model_calls(space_id, entry_id, processing_attempt, span_id);
CREATE TABLE model_call_contents (
 id uuid PRIMARY KEY REFERENCES model_calls(id) ON DELETE CASCADE,
 request_gzip bytea NOT NULL,
 response_gzip bytea NOT NULL,
 request_bytes bigint NOT NULL,
 response_bytes bigint NOT NULL,
 response_type text NOT NULL,
 http_status integer NOT NULL,
 complete boolean NOT NULL
);
CREATE TABLE model_call_content_deletions (
 space_id bigint NOT NULL,
 entry_id text NOT NULL,
 PRIMARY KEY(space_id, entry_id)
);
-- Review calls can precede entry insertion, so deletion uses a trigger instead of an entry FK.
CREATE FUNCTION delete_entry_model_call_contents() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(OLD.space_id::text || ':' || OLD.id, 24));
 INSERT INTO model_call_content_deletions(space_id,entry_id) VALUES(OLD.space_id,OLD.id) ON CONFLICT DO NOTHING;
 DELETE FROM model_call_contents WHERE id IN (SELECT id FROM model_calls WHERE entry_id=OLD.id AND space_id=OLD.space_id);
 RETURN OLD;
END $$;
CREATE TRIGGER entry_model_call_contents_delete AFTER DELETE ON entries FOR EACH ROW EXECUTE FUNCTION delete_entry_model_call_contents();
