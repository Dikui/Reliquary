BEGIN;
CREATE TABLE public.entity_candidate_entries (
 space_id bigint NOT NULL,
 candidate_id varchar(36) NOT NULL,
 entry_id varchar(36) NOT NULL,
 quote text NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY (candidate_id, entry_id),
 FOREIGN KEY (space_id, candidate_id) REFERENCES public.entity_candidates(space_id, id) ON DELETE CASCADE,
 FOREIGN KEY (space_id, entry_id) REFERENCES public.entries(space_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_entity_candidate_entries_entry ON public.entity_candidate_entries(space_id,entry_id);
COMMIT;
