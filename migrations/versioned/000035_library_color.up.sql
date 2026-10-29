BEGIN;

ALTER TABLE libraries ADD COLUMN color INTEGER;

WITH ordered AS (
    SELECT id, ((row_number() OVER (PARTITION BY space_id ORDER BY created_at, id) - 1) % 8 + 1)::INTEGER AS color
    FROM libraries
)
UPDATE libraries SET color = ordered.color FROM ordered WHERE libraries.id = ordered.id;

ALTER TABLE libraries ALTER COLUMN color SET DEFAULT 1;
ALTER TABLE libraries ALTER COLUMN color SET NOT NULL;
ALTER TABLE libraries ADD CONSTRAINT libraries_color_check CHECK (color BETWEEN 1 AND 8);

COMMIT;
