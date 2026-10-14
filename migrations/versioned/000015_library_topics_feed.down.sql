BEGIN;
DROP TABLE follow_feed_library_states;
ALTER TABLE topics DROP COLUMN library_ids;
COMMIT;
