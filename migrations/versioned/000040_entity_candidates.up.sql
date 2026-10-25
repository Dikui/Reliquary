BEGIN;
ALTER TABLE public.facts
 ADD COLUMN subject_entity_id varchar(36) REFERENCES public.entities(id) ON DELETE SET NULL,
 ADD COLUMN object_entity_id varchar(36) REFERENCES public.entities(id) ON DELETE SET NULL;
CREATE INDEX idx_facts_subject_entity ON public.facts(space_id, subject_entity_id);
CREATE INDEX idx_facts_object_entity ON public.facts(space_id, object_entity_id);
CREATE TABLE public.entity_candidates (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES public.spaces(id),
 name varchar(512) NOT NULL,
 normalized_name varchar(512) NOT NULL CHECK (length(normalized_name)>0),
 entity_type varchar(16) NOT NULL CHECK (entity_type IN ('organization','person')),
 status varchar(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','ignored','approved','aliased')),
 entity_id varchar(36) REFERENCES public.entities(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE (space_id, normalized_name),
 UNIQUE (space_id, id)
);
CREATE TABLE public.entity_candidate_occurrences (
 space_id bigint NOT NULL,
 candidate_id varchar(36) NOT NULL,
 fact_id varchar(36) NOT NULL,
 endpoint varchar(16) NOT NULL CHECK (endpoint IN ('subject','object')),
 created_at timestamptz NOT NULL,
 PRIMARY KEY (candidate_id, fact_id, endpoint),
 FOREIGN KEY (space_id, candidate_id) REFERENCES public.entity_candidates(space_id, id) ON DELETE CASCADE,
 FOREIGN KEY (space_id, fact_id) REFERENCES public.facts(space_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_entity_candidate_occurrences_fact ON public.entity_candidate_occurrences(space_id,fact_id);
COMMIT;
