BEGIN;
-- Bind bridge judgements to the exact source association that produced them.
CREATE TABLE public.story_bridge_evidence (
 decision_id varchar(36) PRIMARY KEY REFERENCES public.grouping_decisions(id) ON DELETE CASCADE,
 space_id bigint NOT NULL,
 source_fact_id varchar(36) NOT NULL,
 source_created_at timestamptz NOT NULL,
 FOREIGN KEY(space_id,source_fact_id) REFERENCES public.facts(space_id,id)
);
CREATE TABLE public.story_links (
 space_id bigint NOT NULL,
 story_id varchar(36) NOT NULL,
 other_story_id varchar(36) NOT NULL,
 PRIMARY KEY(space_id,story_id,other_story_id),
 CHECK (story_id < other_story_id),
 FOREIGN KEY(space_id,story_id) REFERENCES public.stories(space_id,id),
 FOREIGN KEY(space_id,other_story_id) REFERENCES public.stories(space_id,id)
);
COMMIT;
