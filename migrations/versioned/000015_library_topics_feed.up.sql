BEGIN;
ALTER TABLE topics ADD COLUMN library_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(library_ids)='array');
CREATE TABLE follow_feed_library_states (
 space_id bigint NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
 library_id varchar(36) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
 last_viewed_at timestamptz NOT NULL,
 PRIMARY KEY(space_id,library_id)
);
INSERT INTO follow_feed_library_states(space_id,library_id,last_viewed_at)
 SELECT s.space_id,l.id,s.last_viewed_at FROM follow_feed_states s JOIN libraries l ON l.space_id=s.space_id;
COMMIT;
