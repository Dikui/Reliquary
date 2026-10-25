BEGIN;
-- Story subscriptions and retired viewing times cannot be recovered by downgrading.
DELETE FROM public.follows WHERE target_type='story';
ALTER TABLE public.follows DROP COLUMN story_id;
ALTER TABLE public.follows DROP CONSTRAINT follows_target_type_check;
ALTER TABLE public.follows ADD CONSTRAINT follows_target_type_check CHECK (target_type='entity');
CREATE TABLE public.follow_feed_states (
 space_id bigint PRIMARY KEY REFERENCES public.spaces(id) ON DELETE CASCADE,
 last_viewed_at timestamptz NOT NULL
);
CREATE TABLE public.follow_feed_library_states (
 space_id bigint NOT NULL REFERENCES public.spaces(id) ON DELETE CASCADE,
 library_id varchar(36) NOT NULL REFERENCES public.libraries(id) ON DELETE CASCADE,
 last_viewed_at timestamptz NOT NULL,
 PRIMARY KEY(space_id,library_id)
);
COMMIT;
