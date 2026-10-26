BEGIN;
-- The removed classifications cannot be recovered; downgrade uses a placeholder.
ALTER TABLE public.entities ADD COLUMN entity_type varchar(128) NOT NULL DEFAULT 'organization'
 CHECK (entity_type IN ('organization','person'));
ALTER TABLE public.entity_aliases ADD COLUMN entity_type varchar(128) NOT NULL DEFAULT '';
ALTER TABLE public.entity_candidates ADD COLUMN entity_type varchar(16) NOT NULL DEFAULT 'organization'
 CHECK (entity_type IN ('organization','person'));
DROP INDEX public.idx_entities_canonical;
CREATE UNIQUE INDEX idx_entities_canonical ON public.entities(space_id,entity_type,normalized_name) WHERE merged_into_id='';
COMMIT;
