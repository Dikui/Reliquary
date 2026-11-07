CREATE TABLE shelf_document_versions (
 id varchar(36) PRIMARY KEY,
 space_id bigint NOT NULL REFERENCES spaces(id),
 library_id varchar(36) NOT NULL REFERENCES libraries(id),
 file_id varchar(36) NOT NULL,
 owner_id varchar(36) NOT NULL,
 requirement_id varchar(36),
 certificate_number text NOT NULL DEFAULT '',
 attachment_id varchar(36) REFERENCES entity_attachments(id),
 cloud_url text NOT NULL DEFAULT '',
 entry_id varchar(36) REFERENCES entries(id),
 ordinal bigint NOT NULL CHECK (ordinal > 0),
 version_label text NOT NULL,
 stage text NOT NULL CHECK (stage IN ('draft','final')),
 is_current boolean NOT NULL,
 origin text NOT NULL DEFAULT '',
 occurred_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 source jsonb NOT NULL,
 changeset_id varchar(36) NOT NULL REFERENCES shelf_changesets(id) DEFERRABLE INITIALLY DEFERRED,
 parse_status text NOT NULL CHECK (parse_status IN ('not_parsed','completed')),
 conditions jsonb NOT NULL DEFAULT '[]',
 FOREIGN KEY(space_id,library_id,file_id) REFERENCES entities(space_id,library_id,id),
 FOREIGN KEY(space_id,library_id,owner_id) REFERENCES entities(space_id,library_id,id),
 FOREIGN KEY(space_id,library_id,requirement_id) REFERENCES entities(space_id,library_id,id),
 CHECK (attachment_id IS NOT NULL OR cloud_url <> ''),
 UNIQUE(file_id,ordinal)
);
CREATE UNIQUE INDEX shelf_document_current ON shelf_document_versions(file_id) WHERE is_current;
CREATE UNIQUE INDEX shelf_document_requirement_current ON shelf_document_versions(owner_id,requirement_id) WHERE is_current AND requirement_id IS NOT NULL;
CREATE INDEX shelf_document_owner ON shelf_document_versions(space_id,library_id,owner_id);
CREATE UNIQUE INDEX shelf_document_text_entry ON shelf_document_versions(entry_id) WHERE entry_id IS NOT NULL;
