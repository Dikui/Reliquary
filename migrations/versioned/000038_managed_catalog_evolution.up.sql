ALTER TABLE libraries ADD COLUMN product_library_ids jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(product_library_ids)='array');
ALTER TABLE libraries ADD COLUMN document_relation_types jsonb NOT NULL DEFAULT '["references","specification","test_report","comparison"]' CHECK (jsonb_typeof(document_relation_types)='array');
ALTER TABLE library_relation_types ADD COLUMN fields jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(fields)='array');
ALTER TABLE relations ADD COLUMN attributes jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(attributes)='object');
ALTER TABLE entry_entities ADD COLUMN relation_type text NOT NULL DEFAULT 'references';

CREATE TABLE entity_attribute_versions (
 id varchar(36) PRIMARY KEY, space_id bigint NOT NULL REFERENCES spaces(id),
 entity_id varchar(36) NOT NULL REFERENCES entities(id), revision bigint NOT NULL,
 effective_at timestamptz NOT NULL, recorded_at timestamptz NOT NULL,
 attributes jsonb NOT NULL CHECK (jsonb_typeof(attributes)='object'),
 UNIQUE(entity_id,revision)
);
CREATE INDEX entity_attribute_versions_at ON entity_attribute_versions(space_id,entity_id,effective_at DESC,revision DESC);
-- The last saved pre-timeline snapshot is the earliest known state. No
-- earlier attribute history is inferred. Deterministic IDs also support restore.
INSERT INTO entity_attribute_versions(id,space_id,entity_id,revision,effective_at,recorded_at,attributes)
 SELECT md5('entity-attributes:'||e.id)::uuid::text,e.space_id,e.id,e.revision,e.updated_at,e.updated_at,e.attributes
 FROM entities e JOIN libraries l ON l.id=e.library_id WHERE l.entity_mode='managed';

CREATE TABLE entity_reorganizations (
 id varchar(36) PRIMARY KEY, space_id bigint NOT NULL REFERENCES spaces(id),
 library_id varchar(36) NOT NULL REFERENCES libraries(id),
 kind text NOT NULL CHECK(kind IN ('merge','split')),
 source_id varchar(36) NOT NULL, target_id varchar(36) NOT NULL,
 created_at timestamptz NOT NULL,
 FOREIGN KEY(space_id,library_id,source_id) REFERENCES entities(space_id,library_id,id),
 FOREIGN KEY(space_id,library_id,target_id) REFERENCES entities(space_id,library_id,id)
);
CREATE INDEX entity_reorganizations_source ON entity_reorganizations(space_id,source_id);
CREATE INDEX entity_reorganizations_target ON entity_reorganizations(space_id,target_id);

CREATE TABLE entity_attachments (
 id varchar(36) PRIMARY KEY, space_id bigint NOT NULL REFERENCES spaces(id),
 library_id varchar(36) NOT NULL REFERENCES libraries(id),
 name text NOT NULL, mime_type text NOT NULL, size bigint NOT NULL,
 path text NOT NULL, created_at timestamptz NOT NULL
);

CREATE OR REPLACE FUNCTION public.check_catalog_library() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE entry_library varchar(36); owner_library varchar(36); owner_mode text;
BEGIN
 SELECT library_id INTO entry_library FROM entries WHERE id=NEW.entry_id AND space_id=NEW.space_id;
 IF TG_TABLE_NAME='entry_entities' THEN
  SELECT e.library_id,l.entity_mode INTO owner_library,owner_mode FROM entities e JOIN libraries l ON l.id=e.library_id
   WHERE e.id=NEW.entity_id AND e.space_id=NEW.space_id AND l.space_id=NEW.space_id AND l.deleted_at IS NULL;
 ELSE
  SELECT library_id INTO owner_library FROM topics WHERE id=NEW.topic_id AND space_id=NEW.space_id;
 END IF;
 IF entry_library IS NULL OR owner_library IS NULL OR (owner_library IS DISTINCT FROM entry_library AND owner_mode IS DISTINCT FROM 'managed') THEN
  RAISE EXCEPTION 'catalog association must be local or target a managed library in the same space';
 END IF;
 RETURN NEW;
END $$;
