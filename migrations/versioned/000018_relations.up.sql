CREATE TABLE public.relations (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES public.spaces(id) ON DELETE CASCADE,
 library_id varchar(36) NOT NULL REFERENCES public.libraries(id) ON DELETE CASCADE,
 subject_id varchar(36) NOT NULL REFERENCES public.entities(id),
 relation_type varchar(100) NOT NULL,
 object_id varchar(36) NOT NULL REFERENCES public.entities(id),
 version bigint NOT NULL CHECK (version >= 1),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (library_id,subject_id,relation_type,object_id),
 FOREIGN KEY (library_id,relation_type) REFERENCES public.library_relation_types(library_id,name),
 CHECK (subject_id <> object_id)
);
CREATE INDEX relations_subject ON public.relations(space_id,subject_id,relation_type);
CREATE INDEX relations_object ON public.relations(space_id,object_id,relation_type);
CREATE TABLE public.relation_sources (
 relation_id varchar(36) NOT NULL REFERENCES public.relations(id) ON DELETE CASCADE,
 entry_id varchar(36) NOT NULL REFERENCES public.entries(id) ON DELETE CASCADE,
 evidence jsonb NOT NULL CHECK (jsonb_typeof(evidence)='array'),
 run_id text NOT NULL,
 body_hash text NOT NULL,
 PRIMARY KEY (relation_id,entry_id)
);
CREATE INDEX relation_sources_entry ON public.relation_sources(entry_id);
CREATE TABLE public.relation_suppressions (
 space_id bigint NOT NULL REFERENCES public.spaces(id) ON DELETE CASCADE,
 library_id varchar(36) NOT NULL REFERENCES public.libraries(id) ON DELETE CASCADE,
 subject_id varchar(36) NOT NULL REFERENCES public.entities(id),
 relation_type varchar(100) NOT NULL,
 object_id varchar(36) NOT NULL REFERENCES public.entities(id),
 actor text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (library_id,subject_id,relation_type,object_id),
 FOREIGN KEY (library_id,relation_type) REFERENCES public.library_relation_types(library_id,name)
);
CREATE TABLE public.relation_type_merges (
 library_id varchar(36) NOT NULL REFERENCES public.libraries(id) ON DELETE CASCADE,
 from_type varchar(100) NOT NULL,
 to_type varchar(100) NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (library_id,from_type),
 FOREIGN KEY (library_id,from_type) REFERENCES public.library_relation_types(library_id,name),
 FOREIGN KEY (library_id,to_type) REFERENCES public.library_relation_types(library_id,name),
 CHECK (from_type <> to_type)
);
-- No foreign keys to projected objects: tombstones must survive their deletion.
CREATE TABLE public.graph_pending_sync (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL,
 entity_id varchar(36) NOT NULL,
 relation_id varchar(36) NOT NULL,
 relation_version bigint NOT NULL CHECK (relation_version >= 1),
 deleted boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX graph_pending_sync_entity ON public.graph_pending_sync(space_id,entity_id,created_at,id);
