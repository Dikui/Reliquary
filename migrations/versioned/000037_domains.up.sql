BEGIN;
CREATE TABLE public.domains (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES public.spaces(id),
 name varchar(128) NOT NULL,
 normalized_name varchar(128) NOT NULL,
 definition text NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE (space_id, normalized_name),
 UNIQUE (space_id, id)
);
CREATE TABLE public.entry_domains (
 space_id bigint NOT NULL,
 entry_id varchar(36) NOT NULL REFERENCES public.entries(id) ON DELETE CASCADE,
 domain_id varchar(36) NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY (entry_id, domain_id),
 FOREIGN KEY (space_id, domain_id) REFERENCES public.domains(space_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_entry_domains_domain ON public.entry_domains(space_id, domain_id, entry_id);
CREATE TABLE public.domain_seed_imports (
 space_id bigint PRIMARY KEY REFERENCES public.spaces(id),
 imported_at timestamptz NOT NULL
);
ALTER TABLE public.entries ADD COLUMN domains_manual boolean NOT NULL DEFAULT false;
COMMIT;
