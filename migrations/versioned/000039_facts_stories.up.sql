BEGIN;
CREATE TABLE public.stories (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES public.spaces(id),
 title varchar(255) NOT NULL,
 first_fact_at timestamptz NOT NULL,
 last_fact_at timestamptz NOT NULL,
 UNIQUE (space_id, id)
);
CREATE INDEX idx_stories_recent ON public.stories(space_id, last_fact_at DESC, id);
CREATE TABLE public.facts (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL,
 story_id varchar(36) NOT NULL,
 title varchar(255) NOT NULL,
 subject text NOT NULL,
 action text NOT NULL,
 object text NOT NULL DEFAULT '',
 conditions text NOT NULL DEFAULT '',
 -- Keep calendar dates distinct from instants; do not invent a time or zone.
 occurred_at text CHECK (occurred_at IS NULL OR (
  occurred_at ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}(T[0-9]{2}:[0-9]{2}:[0-9]{2}([.][0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2}))?$'
  AND occurred_at::timestamptz IS NOT NULL
 )),
 created_at timestamptz NOT NULL,
 UNIQUE (space_id, id),
 FOREIGN KEY (space_id, story_id) REFERENCES public.stories(space_id, id)
);
CREATE INDEX idx_facts_story ON public.facts(space_id, story_id, occurred_at, created_at, id);
CREATE TABLE public.entry_facts (
 space_id bigint NOT NULL,
 entry_id varchar(36) NOT NULL REFERENCES public.entries(id) ON DELETE CASCADE,
 fact_id varchar(36) NOT NULL,
 role varchar(16) NOT NULL CHECK (role IN ('source', 'mention')),
 quote text NOT NULL DEFAULT '',
 manual boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL,
 PRIMARY KEY (entry_id, fact_id),
 FOREIGN KEY (space_id, fact_id) REFERENCES public.facts(space_id, id),
 CHECK (role <> 'source' OR length(quote) > 0)
);
CREATE UNIQUE INDEX idx_entry_facts_one_source ON public.entry_facts(entry_id) WHERE role='source';
CREATE INDEX idx_entry_facts_fact ON public.entry_facts(space_id, fact_id, role);
COMMIT;
