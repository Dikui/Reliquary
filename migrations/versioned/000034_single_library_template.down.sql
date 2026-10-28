-- Development only. Historical template choices cannot be reconstructed;
-- use the previous generic own template without changing content or indexing.
BEGIN;
ALTER TABLE libraries DROP CONSTRAINT libraries_template_check;
ALTER TABLE libraries DROP CONSTRAINT libraries_source_evidence_check;
ALTER TABLE libraries DISABLE TRIGGER protect_library_identity;
UPDATE libraries SET template='own';
ALTER TABLE libraries ENABLE TRIGGER protect_library_identity;
ALTER TABLE libraries ALTER COLUMN template SET DEFAULT 'own';
ALTER TABLE libraries ADD CONSTRAINT libraries_template_check CHECK(template IN ('external_timely','external_durable','own','generated'));
ALTER TABLE libraries ADD CONSTRAINT libraries_source_evidence_check CHECK((template='external_timely' AND source_evidence_rule IN ('link_required','link_or_publisher_date')) OR (template<>'external_timely' AND source_evidence_rule=''));
COMMIT;
