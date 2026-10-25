BEGIN;
CREATE TABLE public.fact_embeddings (
 space_id bigint NOT NULL,
 fact_id varchar(36) PRIMARY KEY,
 model_id varchar(36) NOT NULL,
 dimension integer NOT NULL CHECK (dimension > 0),
 content_hash text NOT NULL,
 embedding halfvec NOT NULL,
 FOREIGN KEY(space_id,fact_id) REFERENCES public.facts(space_id,id) ON DELETE CASCADE,
 CHECK (vector_dims(embedding)=dimension)
);
CREATE TABLE public.grouping_decisions (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL,
 entry_id varchar(36) NOT NULL REFERENCES public.entries(id) ON DELETE CASCADE,
 candidate_fact_id varchar(36) NOT NULL,
 similarity double precision NOT NULL CHECK (similarity BETWEEN -1 AND 1),
 model_id varchar(36) NOT NULL,
 result text NOT NULL CHECK (result IN ('same_occurrence','same_story','unrelated','mention')),
 confidence double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
 review_result text NOT NULL DEFAULT '' CHECK (review_result IN ('','same_occurrence','same_story','unrelated','mention')),
 review_confidence double precision CHECK (review_confidence BETWEEN 0 AND 1),
 applied_result text NOT NULL CHECK (applied_result IN ('same_occurrence','same_story','unrelated','mention')),
 reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL,
 FOREIGN KEY(space_id,candidate_fact_id) REFERENCES public.facts(space_id,id)
);
CREATE INDEX idx_grouping_decisions_entry ON public.grouping_decisions(space_id,entry_id,created_at,id);
COMMIT;
