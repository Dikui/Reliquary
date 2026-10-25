BEGIN;
ALTER TABLE public.follows DROP CONSTRAINT follows_target_type_check;
ALTER TABLE public.follows ADD CONSTRAINT follows_target_type_check CHECK (target_type IN ('entity','story'));
ALTER TABLE public.follows ADD COLUMN story_id varchar(36)
 GENERATED ALWAYS AS (CASE WHEN target_type='story' THEN target_id END) STORED;
ALTER TABLE public.follows ADD CONSTRAINT follows_space_id_story_id_fkey
 FOREIGN KEY (space_id,story_id) REFERENCES public.stories(space_id,id) ON DELETE CASCADE;
DROP TABLE public.follow_feed_library_states;
DROP TABLE public.follow_feed_states;
COMMIT;
