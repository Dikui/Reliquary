-- Certification identity comes from its record key, not its display name.
-- Codes and all other managed and extracted name scopes retain their indexes.
DROP INDEX entities_managed_name;
CREATE UNIQUE INDEX entities_managed_name ON entities(space_id,library_id,managed_kind,
 (CASE WHEN managed_kind='document_requirement' THEN COALESCE(attributes #>> '{requirement,list_name}','') ELSE '' END),normalized_name)
 WHERE archived_at IS NULL AND merged_into_id='' AND managed_kind IS NOT NULL AND managed_kind<>'certification';
