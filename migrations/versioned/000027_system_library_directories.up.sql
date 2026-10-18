BEGIN;

ALTER TABLE libraries DISABLE TRIGGER protect_library_identity;

WITH renamed(old_name,new_name) AS (VALUES
 ('institutional','official'),('opinions','media'),('mine','personal'),('ai-reports','ai-output')
)
UPDATE entries e
SET vault_path=r.new_name||substr(e.vault_path,length(r.old_name)+1),
    source=CASE WHEN e.source=e.vault_path THEN r.new_name||substr(e.vault_path,length(r.old_name)+1) ELSE e.source END
FROM libraries l,renamed r
WHERE e.library_id=l.id AND l.system_role=r.old_name AND l.directory=r.old_name
  AND e.vault_path LIKE r.old_name||'/%';

WITH renamed(old_name,new_name) AS (VALUES
 ('institutional','official'),('opinions','media'),('mine','personal'),('ai-reports','ai-output')
)
UPDATE libraries l
SET system_role=r.new_name,
    directory=CASE WHEN l.directory=r.old_name THEN r.new_name ELSE l.directory END
FROM renamed r WHERE l.system_role=r.old_name;

ALTER TABLE libraries ENABLE TRIGGER protect_library_identity;

COMMIT;
