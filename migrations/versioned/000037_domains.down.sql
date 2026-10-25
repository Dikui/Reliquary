BEGIN;
ALTER TABLE public.entries DROP COLUMN domains_manual;
DROP TABLE public.entry_domains;
DROP TABLE public.domain_seed_imports;
DROP TABLE public.domains;
COMMIT;
