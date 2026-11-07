-- The derived discriminator keeps extracted-library indexes unchanged while
-- managed identities use their configured kind, including converted identities.
ALTER TABLE entities ADD COLUMN managed_kind text;
CREATE FUNCTION set_entity_managed_kind() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT CASE WHEN entity_mode='managed' THEN NEW.kind ELSE NULL END
 INTO NEW.managed_kind FROM libraries WHERE id=NEW.library_id;
 RETURN NEW;
END $$;
CREATE TRIGGER entities_managed_kind BEFORE INSERT OR UPDATE ON entities
 FOR EACH ROW EXECUTE FUNCTION set_entity_managed_kind();
UPDATE entities e SET managed_kind=e.kind FROM libraries l
 WHERE l.id=e.library_id AND l.entity_mode='managed';

DROP INDEX idx_entities_canonical;
CREATE UNIQUE INDEX idx_entities_canonical ON entities(space_id,library_id,entity_type,normalized_name)
 WHERE archived_at IS NULL AND merged_into_id='' AND managed_kind IS NULL;
CREATE UNIQUE INDEX entities_managed_name ON entities(space_id,library_id,managed_kind,normalized_name)
 WHERE archived_at IS NULL AND merged_into_id='' AND managed_kind IS NOT NULL;
DROP INDEX entities_library_code;
CREATE UNIQUE INDEX entities_library_code ON entities(space_id,library_id,lower(code))
 WHERE code<>'' AND merged_into_id='' AND managed_kind IS NULL;
CREATE UNIQUE INDEX entities_managed_code ON entities(space_id,library_id,managed_kind,lower(code))
 WHERE code<>'' AND merged_into_id='' AND managed_kind IS NOT NULL;

CREATE FUNCTION refresh_library_managed_kind() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.entity_mode IS DISTINCT FROM NEW.entity_mode THEN
  UPDATE entities SET managed_kind=CASE WHEN NEW.entity_mode='managed' THEN kind ELSE NULL END
   WHERE library_id=NEW.id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER libraries_managed_kind AFTER UPDATE OF entity_mode ON libraries
 FOR EACH ROW EXECUTE FUNCTION refresh_library_managed_kind();

CREATE TABLE entity_name_confirmations (
 space_id bigint NOT NULL REFERENCES spaces(id),
 library_id varchar(36) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
 entity_a_id varchar(36) NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
 kind_a text NOT NULL,
 entity_b_id varchar(36) NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
 kind_b text NOT NULL,
 normalized_name varchar(512) NOT NULL,
 confirmed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(space_id,library_id,entity_a_id,kind_a,entity_b_id,kind_b,normalized_name),
 CHECK(entity_a_id<entity_b_id AND kind_a<>kind_b)
);
