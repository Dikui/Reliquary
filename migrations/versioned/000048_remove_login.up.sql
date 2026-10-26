BEGIN;

DROP TABLE public.auth_tokens;
ALTER TABLE public.users DROP COLUMN password_hash;

COMMIT;
