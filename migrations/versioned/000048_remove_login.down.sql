-- Development rollback recreates empty login storage, never old credentials.
BEGIN;

ALTER TABLE public.users ADD COLUMN password_hash character varying(255) NOT NULL DEFAULT '';
ALTER TABLE public.users ALTER COLUMN password_hash DROP DEFAULT;
CREATE TABLE public.auth_tokens (
    id character varying(36) DEFAULT public.uuid_generate_v4() NOT NULL PRIMARY KEY,
    user_id character varying(36) NOT NULL,
    token text NOT NULL,
    token_type character varying(50) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    is_revoked boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_auth_tokens_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE
);
CREATE INDEX idx_auth_tokens_expires_at ON public.auth_tokens (expires_at);
CREATE INDEX idx_auth_tokens_token ON public.auth_tokens (token);
CREATE INDEX idx_auth_tokens_token_type ON public.auth_tokens (token_type);
CREATE INDEX idx_auth_tokens_user_id ON public.auth_tokens (user_id);

COMMIT;
