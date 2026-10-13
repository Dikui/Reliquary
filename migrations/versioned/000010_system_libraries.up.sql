BEGIN;
DROP INDEX IF EXISTS idx_libraries_active_space;
ALTER TABLE spaces ADD COLUMN embedding_model_id varchar(64) NOT NULL DEFAULT '';
UPDATE spaces s SET embedding_model_id=COALESCE(NULLIF(l.model_tiers->>'embedding',''),l.embedding_model_id,'') FROM libraries l WHERE l.space_id=s.id AND l.deleted_at IS NULL;
ALTER TABLE libraries
 ADD COLUMN system_role varchar(32) NOT NULL DEFAULT '',
 ADD COLUMN template varchar(32) NOT NULL DEFAULT 'own',
 ADD COLUMN directory varchar(100) NOT NULL DEFAULT '',
 ADD COLUMN review_criteria text NOT NULL DEFAULT '',
 ADD COLUMN source_evidence_rule varchar(32) NOT NULL DEFAULT '',
 ADD COLUMN feed_enabled boolean NOT NULL DEFAULT false;
UPDATE libraries SET system_role='institutional',template='external_timely',directory='institutional',source_evidence_rule='link_or_publisher_date',feed_enabled=true WHERE deleted_at IS NULL;
UPDATE libraries SET directory='archived-'||md5(id) WHERE directory='';
CREATE UNIQUE INDEX idx_libraries_directory ON libraries(space_id,directory);
CREATE UNIQUE INDEX idx_libraries_system_role ON libraries(space_id,system_role) WHERE system_role<>'' AND deleted_at IS NULL;
CREATE TABLE library_content_types (
 library_id varchar(36) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
 name varchar(100) NOT NULL CHECK (length(btrim(name))>0),
 position integer NOT NULL CHECK(position>=0), enabled boolean NOT NULL DEFAULT true, extract_graph boolean NOT NULL DEFAULT false,
 PRIMARY KEY(library_id,name)
);
CREATE TABLE library_relation_types (
 library_id varchar(36) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
 name varchar(100) NOT NULL CHECK (length(btrim(name))>0),
 position integer NOT NULL CHECK(position>=0), enabled boolean NOT NULL DEFAULT true,
 PRIMARY KEY(library_id,name)
);
UPDATE libraries SET name=CASE WHEN name IN ('库','Library') THEN '机构发布' ELSE name END,description=CASE WHEN COALESCE(description,'')='' THEN '官方、公司、媒体、研究机构发布的新闻、公告、财报、研报、政策。有时效的机构研报收在这里。' ELSE description END,review_criteria='发布者是机构（官方、公司、媒体、研究机构、行业协会）；有实质信息，不是广告、招聘、纯转发；正文完整。' WHERE system_role='institutional';
INSERT INTO library_content_types SELECT id,'新闻',0,true,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'公告',1,true,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'财报',2,true,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'研究报告',3,true,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'政策法规',4,true,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'其他',5,true,false FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'投资',0,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'收购',1,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'合作',2,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'竞争',3,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'任职',4,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'发布',5,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'供应',6,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'客户',7,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'监管',8,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'隶属',9,true FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO libraries(id,space_id,creator_id,name,type,description,system_role,template,directory,review_criteria,source_evidence_rule,feed_enabled,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,created_at,updated_at) SELECT gen_random_uuid()::text,space_id,creator_id,'人物观点','document','外部个人的观点：专家、分析师、KOL 的评论与文章。','opinions','external_timely','opinions','作者是外部个人，必须能确定作者；有明确观点或判断；不是纯转发、无内容的短评。','link_required',true,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,now(),now() FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'评论',0,true,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'分析',1,true,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'预测',2,true,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'访谈',3,true,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'其他',4,true,false FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'看好',0,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'看空',1,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'评价',2,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'预测',3,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'批评',4,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'推荐',5,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'投资',6,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'收购',7,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'合作',8,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'竞争',9,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'任职',10,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'发布',11,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'供应',12,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'客户',13,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'监管',14,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'隶属',15,true FROM libraries WHERE system_role='opinions' AND deleted_at IS NULL;
INSERT INTO libraries(id,space_id,creator_id,name,type,description,system_role,template,directory,review_criteria,source_evidence_rule,feed_enabled,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,created_at,updated_at) SELECT gen_random_uuid()::text,space_id,creator_id,'参考文献','document','长期引用的论文、书籍、标准、官方文档、教程。作为定义和方法长期引用的白皮书收在这里。','references','external_durable','references','长期有参考价值；正文完整，不是摘要或目录。','',false,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,now(),now() FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'论文',0,true,false FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'书籍',1,true,false FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'标准',2,true,false FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'官方文档',3,true,false FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'教程',4,true,false FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'其他',5,true,false FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'提出',0,true FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'引用',1,true FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'属于',2,true FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'依赖',3,true FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'对比',4,true FROM libraries WHERE system_role='references' AND deleted_at IS NULL;
INSERT INTO libraries(id,space_id,creator_id,name,type,description,system_role,template,directory,review_criteria,source_evidence_rule,feed_enabled,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,created_at,updated_at) SELECT gen_random_uuid()::text,space_id,creator_id,'我的资料','document','本人的工作资料、经验、SOP、笔记。','mine','own','mine','正文完整、可读；不是结构化记录（任务清单、工单、明细表）。','',false,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,now(),now() FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'方案',0,true,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'纪要',1,true,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'报告',2,true,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'SOP',3,true,false FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'经验',4,true,false FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'笔记',5,true,false FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_content_types SELECT id,'其他',6,true,false FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'负责',0,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'参与',1,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'合作',2,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'客户',3,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'供应',4,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'依赖',5,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'属于',6,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO library_relation_types SELECT id,'决定',7,true FROM libraries WHERE system_role='mine' AND deleted_at IS NULL;
INSERT INTO libraries(id,space_id,creator_id,name,type,description,system_role,template,directory,review_criteria,source_evidence_rule,feed_enabled,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,created_at,updated_at) SELECT gen_random_uuid()::text,space_id,creator_id,'AI 报告','document','外部 AI 按要求写回的报告；不能当作事实。','ai-reports','generated','ai-reports','不审核、不处理，只建索引。标题、Markdown 正文必填；关联资料、已存在的实体与主题可选。','',false,embedding_model_id,summary_model_id,model_tiers,chunking_config,indexing_strategy,now(),now() FROM libraries WHERE system_role='institutional' AND deleted_at IS NULL;
UPDATE entries e SET library_id=l.id FROM libraries l WHERE l.space_id=e.space_id AND l.deleted_at IS NULL AND l.system_role=CASE e.source_kind WHEN 'mine' THEN 'mine' WHEN 'report' THEN 'ai-reports' ELSE 'institutional' END;
UPDATE chunks c SET library_id=e.library_id FROM entries e WHERE c.entry_id=e.id;
UPDATE article_embeddings c SET library_id=e.library_id FROM entries e WHERE c.entry_id=e.id;
UPDATE image_embeddings c SET library_id=e.library_id FROM entries e WHERE c.entry_id=e.id;
UPDATE entry_repost_sources c SET library_id=e.library_id FROM entries e WHERE c.entry_id=e.id;
UPDATE embeddings c SET library_id=e.library_id FROM entries e WHERE c.entry_id=e.id;
-- Existing queued work must continue to address the entry's migrated library.
UPDATE task_pending_ops q SET payload=jsonb_set(q.payload,'{library_id}',to_jsonb(e.library_id)) FROM entries e WHERE q.payload->>'knowledge_id'=e.id AND q.payload ? 'library_id';
ALTER TABLE libraries ADD CONSTRAINT libraries_template_check CHECK(template IN ('external_timely','external_durable','own','generated'));
ALTER TABLE libraries ADD CONSTRAINT libraries_directory_check CHECK(directory ~ '^[a-z0-9]+(-[a-z0-9]+)*$');
ALTER TABLE libraries ADD CONSTRAINT libraries_source_evidence_check CHECK((template='external_timely' AND source_evidence_rule IN ('link_required','link_or_publisher_date')) OR (template<>'external_timely' AND source_evidence_rule=''));
-- Allow old fixture/internal constructors to omit a directory; HTTP creation requires it.
CREATE FUNCTION library_default_directory() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.directory='' THEN NEW.directory='custom-'||md5(NEW.id); END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER library_default_directory BEFORE INSERT ON libraries FOR EACH ROW EXECUTE FUNCTION library_default_directory();
CREATE FUNCTION protect_library_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.template IS DISTINCT FROM OLD.template OR NEW.directory IS DISTINCT FROM OLD.directory OR NEW.system_role IS DISTINCT FROM OLD.system_role THEN
 RAISE EXCEPTION 'library template, directory and system role are immutable' USING ERRCODE='23514'; END IF;
 IF OLD.system_role<>'' AND NEW.deleted_at IS DISTINCT FROM OLD.deleted_at THEN RAISE EXCEPTION 'system libraries cannot be deleted' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_library_identity BEFORE UPDATE ON libraries FOR EACH ROW EXECUTE FUNCTION protect_library_identity();
CREATE FUNCTION protect_entry_library() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.library_id IS DISTINCT FROM OLD.library_id THEN RAISE EXCEPTION 'entry library_id is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_entry_library BEFORE UPDATE OF library_id ON entries FOR EACH ROW EXECUTE FUNCTION protect_entry_library();
COMMIT;
