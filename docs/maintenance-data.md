# 数据归属与文件库恢复格式

所有 PostgreSQL 非系统普通表及其每列均显式分类：public 表在 `internal/maintenance/rules.go`，扩展拥有的表在 `internal/maintenance/extension_rules.go`。目录核对也遍历分区表、外部表和物化视图，出现未经审核的关系类型立即停止。维护导出遇到未分类 schema、表或列立即报错；测试也检测重复分类、漏列和拼写错误。新增结构必须同时更新分类与逐表恢复测试。当前固定镜像的完整目录为 82 张表、960 列，其中 public 44 张表、515 列，非 public 扩展表 38 张、445 列；扩展拥有表共 39 张，包含 public.spatial_ref_sys。当前维护归属以 `000005` 后的结构为准，已删除 `data_sources` 与 `sync_logs`。下表列出当前 public 的 44 张表。

维护副本与核对清单的 `format_version` 为 5，表名使用 `spaces`、`api_keys`、`libraries`、`entries` 和 `entry_` 关联表，行外层字段使用 `space_id`、`library_id`、`entry_id`、`entry_ids`。导入及核对接受版本 1 至 4：版本 1 先映射个人空间表及归属列，其中版本 1 和 2 将 `knowledges`、四张 `knowledge_` 关联表和行外层 `knowledge_id`、`knowledge_ids` 映射为当前名字。版本 1 至 3 的 `knowledge_bases` 表名与行外层 `knowledge_base_id` 分别映射为 `libraries`、`library_id`。先验证原文件校验和，再在原版本命名下比较规范行哈希，任意嵌套 JSON 不递归改名。文件库表头、任务 payload、日志的 before/after、其他嵌套 JSON 与设置键保持原值。只追加的 `operation_logs.jsonl` 保留旧外层归属键，SQL 列与网页 HTTP 字段使用新名字。下一次刷新副本写入版本 5，完成清单原子替换后才移除旧表文件。标题与摘要向量 `article_embeddings` 包含文章和导入的用户文档，不包含整理结果；表名保留，外键列为 `entry_id`。

个人空间命名迁移仅改名和重建引用旧列的函数及 BM25 索引，分类数量不变，当时的目录为 82 张表、968 列，见 [命名清理证据](tasks/naming-02/evidence/schema-statistics.json)。迁移 `000004_ingest_only` 新增 2 张表与 27 列，当时为 84 张表、995 列。迁移 `000005_remove_collection` 删除采集配置与同步日志两表，表内专属列与索引随表删除；历史文章来源字段保留。

离线维护按数据库记录的迁移版本选择归属清单。版本 0 至 3 使用 `000004` 之前的严格列集合，仍核对未知表与未知列；不会为了读取新增字段而升级旧数据库，也不更改旧迁移历史。旧命名适配仍只在同一事务内临时转换并恢复，版本 4 使用删表前的完整清单，版本 5 至 7 使用当前清单；版本 7 之前的库名在离线事务中临时适配，并在事务结束前还原。旧结构完整备份仍包括两张采集表，恢复后在迁移前导出。

| 表 | 在表头 | 在 `.reliquary/` | 可重新生成 | 属于配置或密钥 |
| --- | --- | --- | --- | --- |
| `article_embeddings` | 无 | 无 | `entry_id`、`space_id`、`library_id`、`model_id`、`dimension`、`embedding`、`content_hash`、`created_at`、`updated_at` | 无 |
| `auth_tokens` | 无 | `id`、`user_id`、`token_type`、`expires_at`、`is_revoked`、`created_at`、`updated_at` | 无 | `token` |
| `chatgpt_accounts` | 无 | 无 | 无 | `id`、`client_id`、`issuer`、`subject`、`email`、`credentials`、`scopes`、`expires_at`、`earliest_refresh_at`、`status`、`last_error`、`welcome_acknowledged`、`created_at`、`updated_at` |
| `chatgpt_hosts` | 无 | 无 | 无 | `id`、`ext_agent_host_id`、`created_at` |
| `chatgpt_login_attempts` | 无 | `callback_url`、`client_id`、`account_id`、`expires_at` | 无 | `state`、`nonce`、`verifier` |
| `chatgpt_policy` | 无 | 无 | 无 | `id`、`daily_limit`、`daily_used`、`day`、`paused`、`reason`、`retry_at`、`fallback_model_id`、`updated_at` |
| `chunks` | 无 | 无 | `id`、`space_id`、`library_id`、`entry_id`、`content`、`chunk_index`、`is_enabled`、`start_at`、`end_at`、`pre_chunk_id`、`next_chunk_id`、`chunk_type`、`parent_chunk_id`、`image_info`、`relation_chunks`、`indirect_relation_chunks`、`created_at`、`updated_at`、`deleted_at`、`metadata`、`status`、`content_hash`、`flags`、`seq_id`、`video_info`、`context_header`、`search_title` | 无 |
| `embedding_rebuild_items` | 无 | `id`、`run_id`、`kind`、`source_id`、`attachment_path`、`status`、`attempts`、`error`、`created_at`、`updated_at` | 无 | 无 |
| `embedding_rebuild_runs` | 无 | `id`、`space_id`、`status`、`model_id`、`model_name`、`dimension`、`profile_hash`、`total_count`、`completed_count`、`failed_count`、`error`、`attempt_id`、`lease_until`、`created_at`、`updated_at`、`started_at`、`finished_at` | 无 | 无 |
| `embeddings` | 无 | 无 | `id`、`created_at`、`updated_at`、`source_id`、`source_type`、`chunk_id`、`entry_id`、`library_id`、`model_id`、`dimension`、`embedding`、`is_enabled` | 无 |
| `entities` | 无 | `id`、`space_id`、`canonical_name`、`normalized_name`、`entity_type`、`description`、`merged_into_id`、`created_at`、`updated_at` | `name_embedding`、`embedding_model_id`、`embedding_dimension`、`embedding_needs_rebuild` | 无 |
| `entity_aliases` | 无 | `id`、`space_id`、`entity_type`、`entity_id`、`alias`、`normalized_alias`、`resolution_method`、`created_at` | 无 | 无 |
| `entity_embedding_rebuild_runs` | 无 | `id`、`space_id`、`status`、`reason`、`error`、`model_id`、`dimension`、`profile_hash`、`processed_count`、`total_count`、`model_calls`、`attempts`、`attempt_id`、`created_at`、`updated_at`、`started_at`、`finished_at` | 无 | 无 |
| `entity_merge_records` | 无 | `id`、`space_id`、`keep_entity_id`、`merge_entity_id`、`actor`、`created_at` | 无 | 无 |
| `entity_merge_suggestions` | 无 | `id`、`space_id`、`new_entity_id`、`candidate_entity_id`、`entry_id`、`reason`、`status`、`created_at`、`decided_at` | 无 | 无 |
| `follow_feed_states` | 无 | `space_id`、`last_viewed_at` | 无 | 无 |
| `follows` | 无 | `space_id`、`target_type`、`target_id`、`created_at` | `entity_id`、`topic_id` | 无 |
| `image_embeddings` | 无 | 无 | `entry_id`、`space_id`、`library_id`、`attachment_path`、`model_id`、`dimension`、`embedding`、`content_hash`、`created_at`、`updated_at` | 无 |
| `ingest_assets` | 无 | `id`、`space_id`、`name`、`file_type`、`size`、`path`、`created_at`、`referenced_at`、`entry_id` | 无 | 无 |
| `ingest_discards` | 无 | `space_id`、`normalized_url`、`article_id`、`discarded_at`、`reason` | 无 | 无 |
| `libraries` | 无 | 无 | 无 | `model_tiers`、`id`、`name`、`description`、`space_id`、`chunking_config`、`embedding_model_id`、`summary_model_id`、`created_at`、`updated_at`、`deleted_at`、`type`、`indexing_strategy`、`creator_id` |
| `entry_entities` | 无 | `space_id`、`entry_id`、`entity_id`、`created_at` | 无 | 无 |
| `entry_processing_spans` | 无 | `id`、`entry_id`、`attempt`、`span_id`、`parent_span_id`、`name`、`kind`、`status`、`input`、`output`、`metadata`、`error_code`、`error_message`、`error_detail`、`started_at`、`finished_at`、`duration_ms`、`created_at`、`updated_at` | 无 | 无 |
| `entry_repost_sources` | 无 | `id`、`space_id`、`library_id`、`entry_id`、`original_url`、`normalized_url`、`source_fingerprint`、`author`、`publisher`、`published_at`、`published_at_missing`、`match_method`、`sim_hash_distance`、`created_at` | 无 | 无 |
| `entry_topics` | 无 | `space_id`、`entry_id`、`topic_id`、`source`、`is_historical`、`created_at` | 无 | 无 |
| `entries` | `title`、`content_type`、`source_unverified`、`starred`、`collect_reason`、`structured_summary`、`created_at`、`acquisition` | `id`、`space_id`、`library_id`、`type`、`description`、`source_kind`、`vault_path`、`vault_body`、`agent_summary`、`agent_tags`、`source_name`、`source`、`parse_status`、`enable_status`、`file_name`、`file_type`、`file_size`、`file_path`、`file_hash`、`storage_size`、`metadata`、`updated_at`、`processed_at`、`error_message`、`deleted_at`、`summary_status`、`channel`、`pending_subtasks_count`、`custom_metadata`、`follow_ready_at`、`content_type_corrected`、`normalized_url`、`content_hash`、`content_sim_hash`、`content_rune_count`、`incomplete`、`external_completion_attempted`、`ingest_api_key_id`、`ingest_api_key_name`、`last_ingest_api_key_id`、`last_ingest_api_key_name`、`missing_materials`、`content_type_hint`、`related_article_id` | `search_seq_id`、`search_text`、`search_published_at`、`embedding_model_id` | 无 |
| `maintenance_state` | 无 | `backup_at`、`backup_status`、`backup_path`、`backup_size_bytes`、`backup_error`、`drill_at`、`drill_status`、`drill_error` | `id`、`paused`、`pause_mode`、`pause_reason`、`paused_at`、`updated_at`、`backup_due_at`、`drill_due_at` | `backup_schedule`、`drill_schedule`、`schedule_timezone`、`backup_directory`、`automatic_enabled` |
| `models` | 无 | 无 | 无 | `id`、`space_id`、`name`、`display_name`、`type`、`source`、`description`、`parameters`、`is_default`、`status`、`created_at`、`updated_at`、`deleted_at`、`is_builtin`、`managed_by` |
| `operation_logs` | 无 | `id`、`space_id`、`actor`、`api_key_id`、`api_key_name`、`operation_type`、`object_id`、`before_value`、`after_value`、`created_at` | 无 | 无 |
| `resource_access_grants` | 无 | `id`、`resource_id`、`access_scope`、`expires_at`、`revoked_at`、`created_at` | 无 | `token_hash` |
| `resource_bindings` | 无 | `id`、`resource_id`、`space_id`、`owner_type`、`owner_id`、`relation`、`created_at` | 无 | 无 |
| `resources` | 无 | `id`、`handle`、`space_id`、`provider`、`physical_path`、`location_hash`、`kind`、`mime_type`、`original_name`、`size`、`content_hash`、`lifecycle`、`expires_at`、`state`、`created_at`、`updated_at`、`deleted_at` | 无 | 无 |
| `schema_migrations` | 无 | 无 | `version`、`dirty` | 无 |
| `spatial_ref_sys` | 无 | 无 | `srid`、`auth_name`、`auth_srid`、`srtext`、`proj4text` | 无 |
| `system_settings` | 无 | 无 | 无 | `id`、`key`、`value`、`value_type`、`category`、`description`、`is_secret`、`requires_restart`、`last_modified_by`、`created_at`、`updated_at` |
| `task_dead_letters` | 无 | `id`、`space_id`、`task_type`、`scope`、`scope_id`、`related_id`、`payload`、`last_error`、`fail_count`、`failed_at` | 无 | 无 |
| `task_pending_ops` | 无 | `id`、`task_type`、`object_id`、`generation`、`delivery`、`queue`、`payload`、`headers`、`state`、`max_retry`、`retried`、`fail_count`、`timeout_ns`、`retention_ns`、`deadline`、`group_name`、`created_at`、`updated_at`、`process_at`、`dispatched_at`、`started_at`、`completed_at`、`last_failed_at`、`last_error`、`worker_pool`、`cancel_requested` | 无 | 无 |
| `task_queue_counters` | 无 | `queue`、`day`、`processed`、`failed` | 无 | 无 |
| `api_keys` | 无 | 无 | 无 | `id`、`space_id`、`name`、`key_hash`、`api_key`、`last_used_at`、`expires_at`、`revoked_at`、`created_at`、`updated_at` |
| `spaces` | 无 | 无 | 无 | `id`、`name`、`description`、`status`、`business`、`storage_used`、`created_at`、`updated_at`、`deleted_at`、`retrieval_config`、`credentials` |
| `topic_backfill_batches` | 无 | `id`、`job_id`、`ordinal`、`entry_ids`、`article_count`、`status`、`attempts`、`error`、`created_at`、`updated_at`、`retry_at` | 无 | 无 |
| `topic_backfill_jobs` | 无 | `id`、`space_id`、`topic_id`、`topic_name`、`topic_description`、`status`、`total`、`processed`、`failed_articles`、`failed_batches`、`created_at`、`updated_at` | 无 | 无 |
| `topics` | 无 | `id`、`space_id`、`name`、`description`、`created_at`、`updated_at` | 无 | 无 |
| `users` | 无 | 无 | 无 | `id`、`username`、`email`、`password_hash`、`avatar`、`space_id`、`is_active`、`created_at`、`updated_at`、`deleted_at` |

非 public 扩展表的逐列归属如下。表头、实时副本和配置均不另存这些行；可重新生成分类只在扩展拥有者、固定版本、关系类型、完整列集合、行数与默认内容 SHA256 全部一致时成立。扩展 SQL 安装脚本是默认内容的来源，不凭表名把自定义数据当作缓存。目录包含没有列的空表，新增其他 schema 下的表也不能绕过检查。

| 表 | 在表头 | 在 `.reliquary/` | 可重新生成的全部列 | 属于配置或密钥 | 扩展及固定版本 |
| --- | --- | --- | --- | --- | --- |
| `paradedb._typmod_cache` | 无 | 无 | `id`、`typmod` | 无 | `pg_search 0.22.2` |
| `pgivm.pg_ivm_immv` | 无 | 无 | `immvrelid`、`viewdef`、`ispopulated`、`lastivmupdate` | 无 | `pg_ivm 1.13` |
| `tiger.addr` | 无 | 无 | `gid`、`tlid`、`fromhn`、`tohn`、`side`、`zip`、`plus4`、`fromtyp`、`totyp`、`fromarmid`、`toarmid`、`arid`、`mtfcc`、`statefp` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.addrfeat` | 无 | 无 | `gid`、`tlid`、`statefp`、`aridl`、`aridr`、`linearid`、`fullname`、`lfromhn`、`ltohn`、`rfromhn`、`rtohn`、`zipl`、`zipr`、`edge_mtfcc`、`parityl`、`parityr`、`plus4l`、`plus4r`、`lfromtyp`、`ltotyp`、`rfromtyp`、`rtotyp`、`offsetl`、`offsetr`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.bg` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tractce`、`blkgrpce`、`bg_id`、`namelsad`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.county` | 无 | 无 | `gid`、`statefp`、`countyfp`、`countyns`、`cntyidfp`、`name`、`namelsad`、`lsad`、`classfp`、`mtfcc`、`csafp`、`cbsafp`、`metdivfp`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.county_lookup` | 无 | 无 | `st_code`、`state`、`co_code`、`name` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.countysub_lookup` | 无 | 无 | `st_code`、`state`、`co_code`、`county`、`cs_code`、`name` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.cousub` | 无 | 无 | `gid`、`statefp`、`countyfp`、`cousubfp`、`cousubns`、`cosbidfp`、`name`、`namelsad`、`lsad`、`classfp`、`mtfcc`、`cnectafp`、`nectafp`、`nctadvfp`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.direction_lookup` | 无 | 无 | `name`、`abbrev` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.edges` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tlid`、`tfidl`、`tfidr`、`mtfcc`、`fullname`、`smid`、`lfromadd`、`ltoadd`、`rfromadd`、`rtoadd`、`zipl`、`zipr`、`featcat`、`hydroflg`、`railflg`、`roadflg`、`olfflg`、`passflg`、`divroad`、`exttyp`、`ttyp`、`deckedroad`、`artpath`、`persist`、`gcseflg`、`offsetl`、`offsetr`、`tnidf`、`tnidt`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.faces` | 无 | 无 | `gid`、`tfid`、`statefp00`、`countyfp00`、`tractce00`、`blkgrpce00`、`blockce00`、`cousubfp00`、`submcdfp00`、`conctyfp00`、`placefp00`、`aiannhfp00`、`aiannhce00`、`comptyp00`、`trsubfp00`、`trsubce00`、`anrcfp00`、`elsdlea00`、`scsdlea00`、`unsdlea00`、`uace00`、`cd108fp`、`sldust00`、`sldlst00`、`vtdst00`、`zcta5ce00`、`tazce00`、`ugace00`、`puma5ce00`、`statefp`、`countyfp`、`tractce`、`blkgrpce`、`blockce`、`cousubfp`、`submcdfp`、`conctyfp`、`placefp`、`aiannhfp`、`aiannhce`、`comptyp`、`trsubfp`、`trsubce`、`anrcfp`、`ttractce`、`tblkgpce`、`elsdlea`、`scsdlea`、`unsdlea`、`uace`、`cd111fp`、`sldust`、`sldlst`、`vtdst`、`zcta5ce`、`tazce`、`ugace`、`puma5ce`、`csafp`、`cbsafp`、`metdivfp`、`cnectafp`、`nectafp`、`nctadvfp`、`lwflag`、`offset`、`atotal`、`intptlat`、`intptlon`、`the_geom`、`tractce20`、`blkgrpce20`、`blockce20`、`countyfp20`、`statefp20` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.featnames` | 无 | 无 | `gid`、`tlid`、`fullname`、`name`、`predirabrv`、`pretypabrv`、`prequalabr`、`sufdirabrv`、`suftypabrv`、`sufqualabr`、`predir`、`pretyp`、`prequal`、`sufdir`、`suftyp`、`sufqual`、`linearid`、`mtfcc`、`paflag`、`statefp` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.geocode_settings` | 无 | 无 | `name`、`setting`、`unit`、`category`、`short_desc` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.geocode_settings_default` | 无 | 无 | `name`、`setting`、`unit`、`category`、`short_desc` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.loader_lookuptables` | 无 | 无 | `process_order`、`lookup_name`、`table_name`、`single_mode`、`load`、`level_county`、`level_state`、`level_nation`、`post_load_process`、`single_geom_mode`、`insert_mode`、`pre_load_process`、`columns_exclude`、`website_root_override` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.loader_platform` | 无 | 无 | `os`、`declare_sect`、`pgbin`、`wget`、`unzip_command`、`psql`、`path_sep`、`loader`、`environ_set_command`、`county_process_command` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.loader_variables` | 无 | 无 | `tiger_year`、`website_root`、`staging_fold`、`data_schema`、`staging_schema` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.pagc_gaz` | 无 | 无 | `id`、`seq`、`word`、`stdword`、`token`、`is_custom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.pagc_lex` | 无 | 无 | `id`、`seq`、`word`、`stdword`、`token`、`is_custom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.pagc_rules` | 无 | 无 | `id`、`rule`、`is_custom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.place` | 无 | 无 | `gid`、`statefp`、`placefp`、`placens`、`plcidfp`、`name`、`namelsad`、`lsad`、`classfp`、`cpi`、`pcicbsa`、`pcinecta`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.place_lookup` | 无 | 无 | `st_code`、`state`、`pl_code`、`name` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.secondary_unit_lookup` | 无 | 无 | `name`、`abbrev` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.state` | 无 | 无 | `gid`、`region`、`division`、`statefp`、`statens`、`stusps`、`name`、`lsad`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.state_lookup` | 无 | 无 | `st_code`、`name`、`abbrev`、`statefp` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.street_type_lookup` | 无 | 无 | `name`、`abbrev`、`is_hw` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.tabblock` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tractce`、`blockce`、`tabblock_id`、`name`、`mtfcc`、`ur`、`uace`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.tabblock20` | 无 | 无 | `statefp`、`countyfp`、`tractce`、`blockce`、`geoid`、`name`、`mtfcc`、`ur`、`uace`、`uatype`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom`、`housing`、`pop` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.tract` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tractce`、`tract_id`、`name`、`namelsad`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.zcta5` | 无 | 无 | `gid`、`statefp`、`zcta5ce`、`classfp`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`partflg`、`the_geom` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.zip_lookup` | 无 | 无 | `zip`、`st_code`、`state`、`co_code`、`county`、`cs_code`、`cousub`、`pl_code`、`place`、`cnt` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.zip_lookup_all` | 无 | 无 | `zip`、`st_code`、`state`、`co_code`、`county`、`cs_code`、`cousub`、`pl_code`、`place`、`cnt` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.zip_lookup_base` | 无 | 无 | `zip`、`state`、`county`、`city`、`statefp` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.zip_state` | 无 | 无 | `zip`、`stusps`、`statefp` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `tiger.zip_state_loc` | 无 | 无 | `zip`、`stusps`、`statefp`、`place` | 无 | `postgis_tiger_geocoder 3.6.2` |
| `topology.layer` | 无 | 无 | `topology_id`、`layer_id`、`schema_name`、`table_name`、`feature_column`、`feature_type`、`level`、`child_id` | 无 | `postgis_topology 3.6.2` |
| `topology.topology` | 无 | 无 | `id`、`name`、`srid`、`precision`、`hasz`、`useslargeids` | 无 | `postgis_topology 3.6.2` |

public.spatial_ref_sys 的五列也遵守同样条件。当前默认空间参考系为 8500 行，任何新增、自定义或改写的空间参考系都使维护命令停止；tiger 的地理数据、词典、加载配置和 topology、pgivm 的定义也逐表检查默认内容。新增 GIS 数据、修改 geocode_settings、添加自定义 pagc 词典、IVM 定义或 topology 记录时，程序拒绝 Capture、scrub 和打包，保留原数据库；须先为这些真实数据增加显式保存与恢复支持，不能删除后继续或把其列静默归为派生数据。本次不支持 GIS 业务恢复。

ParadeDB 的 paradedb._typmod_cache 保存持久 tokenizer 类型配置。当前应用的固定全文索引 DDL 不调用其配置保存接口，默认清单要求该表为空。普通全文索引数据通过已审核的应用索引 DDL 重新生成；非空 typmod 配置不会因为扩展拥有该表而被忽略，必须先审核如何从代码及索引定义重建，再增加支持。

默认行数与完整 SHA256 固定于 extension_rules.go，安装脚本取证和只读目录取证保存在 `.scratch/kb-reshape/evidence/15/extension-catalog-ddl.txt`、`extension-catalog-columns.txt` 与 `extension-catalog-default-sha256.txt`。哈希使用每行 to_jsonb 的 UTF-8 文本，按 COLLATE C 排序后以换行连接再计算 SHA256，避免机器默认 locale 的排序差异。下表列出默认行数；表为空也核对 SHA256，不只检查行数。

| 扩展表 | 默认行数 | 默认 SHA256 |
| --- | --- | --- |
| `paradedb._typmod_cache` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `pgivm.pg_ivm_immv` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `public.spatial_ref_sys` | 8500 | `94e0d21887c82926e1f5278a9764058bf92c3687c8835d1989c2675d44045dd0` |
| `tiger.addr` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.addrfeat` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.bg` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.county` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.county_lookup` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.countysub_lookup` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.cousub` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.direction_lookup` | 28 | `a4fa8175cbedf5805e26742bd3953594ea86887f463361ee7a8beb61f74418f4` |
| `tiger.edges` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.faces` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.featnames` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.geocode_settings` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.geocode_settings_default` | 7 | `b135a559f2bc0c8f63e0733a033d193045a3d9bcc224cd6edec9460df69d2311` |
| `tiger.loader_lookuptables` | 13 | `ae2950b33699d9c171d30b2c8e7921c9037bfc557e2911bf06e825d7a367c840` |
| `tiger.loader_platform` | 2 | `b8b7753b3784cd7282c3619b12d1bbb27604690029b81bc056cdee8a5513dcd4` |
| `tiger.loader_variables` | 1 | `5ba9d7399bf70dca361bc06cf8ce74f0e467c9a7e88464c154a07e92ee56b3b0` |
| `tiger.pagc_gaz` | 835 | `2d5cc0cabab533bc40cb34a079c71b18b88fc306f7437f768288d181cce475f3` |
| `tiger.pagc_lex` | 2938 | `1ed0619559c6000ac98472c830ce8be41d169e32cb3bbfe24a5eea30ea3e5650` |
| `tiger.pagc_rules` | 4354 | `617b60f850e9dddff15fdb5a18bd8ff49ed516e5f5847d0ed76ac59c080cd045` |
| `tiger.place` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.place_lookup` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.secondary_unit_lookup` | 39 | `46addf8212936dd742ac64939d3090eb2713ad6b9ac97642ea87fa586210df51` |
| `tiger.state` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.state_lookup` | 59 | `a6a3d0d8267bd158dd548301e8d500846686aa0df4000c37cb82f39e720d447b` |
| `tiger.street_type_lookup` | 609 | `b2a5c43ff1ab9048b30cd9c63688a2b8de75d17b871081771eee9ae40b00a84e` |
| `tiger.tabblock` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.tabblock20` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.tract` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.zcta5` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.zip_lookup` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.zip_lookup_all` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.zip_lookup_base` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.zip_state` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `tiger.zip_state_loc` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `topology.layer` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `topology.topology` | 0 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

扩展目录完整性测试遍历全部非系统表和列，验证当前全部表及列都有四类归属；另外验证未知 schema 表、零列表、新列、缺列、改变扩展拥有者、同列物化视图替换，以及各类非默认扩展内容都会失败，失败后原始行不变。扩展默认行不进入应用副本导入事务，新环境由固定版本镜像的扩展安装重新建立，再按同一清单核对。

表头中的文章 ID 同时作为副本各表的关联主键保存。文章 `metadata` 中的来源作者、发布者、发布时间也出现在表头，但原始的收录字段整体保留在副本，避免改写原始来源格式和精度。表头字段在副本中保留一份确定的核对值，导入时检查两份值一致。

`entries.vault_body` 按来源区分：已发布文章的正文从 `articles/` 读取并恢复到数据库；用户文档正文保存在数据库与维护副本中，并与 `mine/` 的表头和正文核对。未发布、待审和处理中内容尚无文件时，在副本保留数据库已有原文与收录 metadata，上传原件保存在资源副本。`reports/` 的原文只在原文件和备份中，数据库与副本不另存原文。派生 chunks 用于检索，不作为恢复原文来源。文件库磁盘损坏且仅数据库尚存时，文章和用户文档可恢复，核对修复会从数据库补回文件；整理结果最多损失上次备份之后的修改。文件库重建按 `mine/` 表头与正文恢复用户文档，没有表头的文件跳过并列出，不报错，不迁移旧文件。pack 与 unpack 同时保存、恢复用户文档的文件、附件和维护副本。

账号密码哈希选择保留在 `config/users.json`，使仅用文件库重建后仍能用原密码登录。密码明文、登录 JWT、API Key、API Key 哈希、资源访问令牌哈希、ChatGPT 令牌和 OAuth 临时 state/nonce/verifier 均不输出。会话与临时访问记录的非密钥元信息全部保留，旧认证值不恢复。账号、模型、解析器配置保留非凭据部分；任意嵌套 JSON 中的凭据键、Authorization/Cookie、URL 用户信息和敏感 query 参数递归剔除。headers 与 custom_headers 只保留 Accept、Content-Type、User-Agent 三种明确公开的头，其他头即使自定义名称不含 token 也列为待填凭据。当前数据库的密文与解密后密钥、SYSTEM_AES_KEY、JWT_SECRET、数据库密码等只在内存中形成核对集合；副本的非密钥字符串中出现同一长凭据时，把对应字串替换为 [credential excluded]，保留其他文字。Basic 或 Bearer 开头的普通文章原文不会仅因开头词语而清空。ChatGPT 凭据中的 token_type、scope、username、email、client_id 等公开元信息不当作秘密字串，避免误删普通内容。被剔除字段写入待配置清单，不保留其值。迁移后模型需要配置、ChatGPT 账号需要重新登录，完成配置后再恢复对应功能。

`.reliquary/` 是数据库的实时可读副本，不反向同步外部编辑。普通表和配置使用 UTF-8 JSON 数组，每行保留原始主键、时间戳和非派生字段；JSON 键稳定排序。操作日志每条一行 JSON，正常变化只追加，不改写既有条目。`verify --repair` 遇到损坏日志时，把坏原件移到 `quarantine/`，保留坏原件，再从数据库建立完整的新日志。这是显式修复操作，正常同步不会做这件事。

```text
.reliquary/
  manifest.json               格式版本、文件及正文哈希、当前上传清单、表清单、凭据待填字段、JSONB null 类型
  config/<table>.json         账号、模型和其他非密钥配置
  config/required_credentials.json  待填字段名的冗余清单与 SHA256，不保存字段值
  data/<table>.json           实体、主题、关注、待审、复核、转载、任务与诊断等
  operation_logs.jsonl        只追加的操作日志
  resources/<relative-path>  上传原件与附件副本，保留 local:// 相对路径
  quarantine/                明确修复操作保留的损坏日志
```

变化通过 GORM 写入回调触发去抖导出，默认与表头相同为 5 秒；定期读取已提交的 PostgreSQL 一致快照捕捉原始 SQL 和队列更新。单文件临时写入、fsync、读回验证、原子替换，内容一致不改 inode。快照 manifest 最后替换，另存根目录除 .reliquary 以外的文章、附件、mine/ 与 reports/ 文件哈希。导入先检查副本和全部原始文件哈希、表清单及持久表的依赖恢复覆盖，有中断、遗漏或篡改时在写入数据库前停止，使用 `verify --repair` 从数据库补齐。

核对逐表比较非派生列的规范 JSON、行数及 JSONB null 类型。JSONB 字面值 null 与 SQL NULL 单独记录，导入后逐列保持原类型，操作日志中的空 before_value 不会误变成 SQL NULL。模型和 ChatGPT 账号的安全行与待填凭据 ID 进入核对清单，原始 status 保留供逐行核对。未剔除凭据的模型，status 与 updated_at 精确比较，任何改变都会失败。只有旧清单已列出的凭据受影响 ID 可以由原 status 转为 models.needs_configuration，再恢复清单记录的原 status；该行 updated_at 允许凭据操作改变。其他 status 不忽略。模型 parameters.extra_config 中 maintenance_required_fields、maintenance_original_status、maintenance_original_updated_at 是恢复提醒，只排除这些明确键，其他参数精确核对。ChatGPT 待重新登录账号允许 needs_login 和完成新登录的 connected，expires_at、earliest_refresh_at 与 updated_at 仅在上述待填账号行允许变化；last_error 仍精确核对。版本 4 及以前的订阅源净化与暂停规则仅用于旧备份读取及删表前导出，当前维护不恢复采集功能。

待处理操作是任务权威，随数据库备份与格式 5 的维护副本保存。启动时 active 或 dispatched 操作恢复为 pending，投递代次 delivery 恰好增加 1，并清空 dispatched_at、worker_pool，更新 updated_at。核对只允许由同 ID 原始行证明的这一转换；payload、generation、object_id、重试次数、截止时间、process_at 和完成历史均精确比较。pending、scheduled、retry、cancelled、archived、completed 的字段不因恢复而放宽。Redis 数据不进入备份，恢复后从未完成操作补推。版本 9 以前的离线维护仍按原 task_jobs 列归属处理，升级 000009 时删除旧任务；不导入旧队列消息。维护暂停标记与定时 due_at 可重新生成，最近备份和恢复演练的时间、状态、目录、大小及错误属于不可再生历史，进入副本并恢复；只有 backup_directory 是目标机器可调整的配置路径。全文索引、分块、向量和搜索 sequence ID 可重新生成，向量值与检索分数不做字节比较。固定检索使用真实 ParadeDB BM25 查询 maintenancearchive、observations、reference，核对排序后的文章 ID，忽略分数。文件仍按字节哈希核对，副本 config 文件由其规范数据库内容核对。正常恢复先与旧清单严格核对，再开放凭据填写；填写过程可以追加新的操作日志，完成凭据配置前再次按旧清单核对。该步骤显式使用 --allow-reconfiguration-append；只允许追加日志的场合使用 --allow-operation-log-append。旧日志按 ID 保留逐行完整内容与 JSONB null 类型，只接受新的日志 ID。--allow-model-change 显式允许库 embedding_model_id 和 model_tiers.embedding 变更，以及旧、新选中 embedding 模型的名称、展示名、说明、来源、参数和 updated_at 变化；原有模型的 ID、租户、类型和创建时间、其他未选模型及全部不可再生业务表仍严格比较。新加模型只允许新选中的 Embedding 行，不允许删掉旧模型。新模型的全部向量生成完整后再开放使用。后续日常 verify 检查当前数据库与副本一致。

迁移清单另存向量语义摘要，包括模型语义 profile、维度、行数和来源覆盖哈希。文章、图片、实体使用原始 ID 和图片路径；分块使用文章 ID、位置与内容哈希，不依赖可重新生成的 chunk ID。`verify --manifest <清单> --vectors` 在模型未改变时严格核对这些摘要，不比较向量浮点值或检索分数。模型改变时先执行完整向量重建，再按新的模型 profile 核对覆盖情况。普通 `verify` 输出 `pending_vectors`，仅用文件库恢复后尚未生成向量时不把原始数据核对放宽，也不把待生成向量误判为原文丢失。

仅用文件库重建在 Import 成功后、索引重建和任何副本 flush 前，重新 Load 尚未覆盖的原副本，逐表核对当前数据库的所有不可再生行、条数与 JSONB null 类型。只使用前文已列出的凭据暂停及任务恢复转换，其他差异直接报错并保留原文件；缺失、多出的持久表也拒绝。不能以 Import 返回的已修改内存行或重新导出的副本作为原始基线。原文件、资源和附件仍先由 Load 核对全部哈希。

导入保持所有业务主键和时间戳，并在同一事务中恢复依赖顺序；`follows` 生成列由 PostgreSQL 重算，历史 `follow_ready_at` 保留，不把恢复误判为新文章。全部 serial sequence 重设到已恢复最大 ID。重建先恢复原始行和全文 chunks，不调用不可用的模型；模型准备完成后通过现有全部向量重建入口恢复向量。

打包最后在原数据库的凭据内存集合下执行 secret-scan，扫描原始文件、Docker save 中的 gzip 层、Git bundle 解压后的全部对象及标准输入的解码 PostgreSQL 导出。不写出或打印核对集合；发现匹配只报告安全的相对文件位置并拒绝打包。密码哈希不加入集合。短凭据不进行任意子串替换，避免破坏主键和普通文字；敏感字段与整个值相等的文本仍剔除，打包扫描对短凭据也不放宽，有泄漏就停止。

原文路径 vault_body、original_content、raw_content、manual.content 与 intake.content 不按 URL 参数名称猜测凭据，公开教程中的 token 或 api_key 示例保持原样；这些原文仍按当前系统的真实凭据集合排除已知密钥。配置、来源 URL 和签名 URL 则剔除用户信息及敏感 query，访问密钥、对象存储密钥与签名字段也进入待填清单。

`verify --vectors` 无论是否提供旧清单，都检查每条现存文章、图片、分块和实体向量的实际 vector_dims、记录的维度以及关联 Embedding 模型的配置维度。同一模型下的现存向量维度必须一致，引用不存在或类型错误的模型也会停止核对。显式切换模型时，只允许追加引用当前选定模型的新向量重建审计运行，新增重建条目必须引用允许的新运行；旧运行和旧条目的内容逐行保留，不借重建过程跳过历史数据核对。

实体的 pending_vectors 检查当前选定模型，而不只检查向量是否存在：旧模型即使维度相同，也仍需重建。未合并实体的模型 ID、配置维度、实际维度与 embedding_needs_rebuild 都必须就绪，合并实体不参与当前覆盖计数。

资源镜像写入前，程序以同一凭据集合核对所有上传原件和文件名。命中当前系统凭据时，整个副本写入失败，不把该原件写进 .reliquary/resources，不改写或删除原上传文件，错误也不包含凭据或原文件名。维护 flush/pack 因而停止并记录失败；先由用户移除原件中的敏感内容或完成凭据处理，再重新核对，不通过自动替换破坏二进制原件。

resources/ 保留完整历史镜像，包括正常处理流程后来移除的原始上传文件。manifest.active_uploads 则另列当前上传目录的全部相对路径、SHA256 和大小，明确空数组表示当前没有上传文件，与旧格式缺少该字段区分。仅用文件库重建时，只把 active_uploads 恢复到新环境上传目录，历史镜像仍留在 .reliquary/resources/，既不删除原件，也不把历史文件重新变成当前上传文件。导入先核对清单路径安全、无重复、每项哈希及大小与已核对历史镜像一致，非法 null 清单也拒绝；任何差异在数据库写入前停止。verify 另行比较清单与实际当前上传目录，迁移核对仍严格比较所有当前上传文件。旧格式没有 active_uploads 时继续按原方式恢复全部历史镜像，无法推断当时哪些文件已退出当前目录，之后应核对清单并人工确认当前文件集合。

认证表的元信息也是不可再生数据。auth_tokens 的用户、类型、过期时间、撤销状态与历史时间，resource_access_grants 的资源、权限范围、过期时间、撤销时间与创建时间，以及 chatgpt_login_attempts 的回调地址、client_id、account_id、过期时间进入 data/<table>.json。token、token_hash、state、nonce、verifier 始终缺席。恢复后的数据库仅为满足原有非空约束写入 reliquary-excluded- 前缀的非认证占位值；该值不具备 JWT 或 SHA256 token_hash 格式。OAuth state 使用安全元信息的规范 JSON 哈希与重复序号，不保存旧 state 或它的哈希，nonce 与 verifier 为空。相同元信息的多个尝试仍保留全部条数。原有 is_revoked、revoked_at 和所有历史时间戳不因失效处理而改写。

初始恢复仍逐表严格核对这些认证元信息。凭据配置期间重新登录或生成新临时访问会新增记录，只有显式 --allow-reconfiguration-append 才接受上述三张表与操作日志的新安全行；auth_tokens 与 resource_access_grants 的旧 ID 行逐列保持，OAuth 尝试按安全元信息多重集合核对旧行和重复次数。新增行不会放宽旧行修改、删除或其他业务表的核对。原 --allow-operation-log-append 仍只允许新日志，未扩大到认证表。

读取版本 4 及以前备份时，旧订阅源的 credentials、鉴权头与配置中的秘密仍递归剔除，导出文件不含可用凭据。当前版本不提供订阅源凭据填写或连接验证入口。

恢复后的无效认证占位值不加入凭据核对集合，否则第二次备份会误判自身占位值为密钥。例外只接受 auth_tokens.token 与 resource_access_grants.token_hash 精确等于 reliquary-excluded- 加该行原 ID；OAuth 必须 nonce、verifier 都为空，state 与该行安全元信息规范哈希及完整重复序号一致。api_keys 只有 api_key 为空且 key_hash 精确等于 disabled- 加该行 ID 才视为无效占位。任何相似前缀、不同 ID、非空 verifier、错误重复序号或仍有 API Key 的记录都继续按真实凭据扫描，原凭据核对不放宽。

解析服务重填仅在旧清单 Required 已列出该 space 的 parser_engine_config.* 凭据路径时允许该行 updated_at 改变。name、status、公共解析器地址与所有非凭据配置仍逐列精确比较，其他租户的提醒也不能放宽当前行。打包后的主副本清单保留原安全字段名；后续 flush 继续保留仍存在租户的解析器原字段提醒。当前数据库的 Required 另行反映实际非空凭据字段，提醒不代替真实填写，功能仍等全部原字段填写完成才恢复。

待填字段名同时保存在 manifest.json 与 config/required_credentials.json，冗余文件内记录规范清单 SHA256，外层 manifest 也记录其文件哈希。主 manifest 损坏而冗余文件有效时，verify --repair 可恢复主清单并保留尚未填写的解析器字段名。两个副本都损坏、数据库又无法推断原缺失字段时，程序拒绝覆盖坏原件，要求从备份恢复清单。旧格式没有冗余文件且原数据库仍含解析器凭据时，可从当前数据库字段重新生成；清单始终只保存字段名，不保存任何凭据值。

维护期间真实 ChatGPT 模型试调用会消耗实际配额，不撤销消耗。配额行有变化时追加 actor=maintenance、operation_type=maintenance_model_probe、object_id=模型 ID 的操作日志，before_value 与 after_value 分别保存完整安全 chatgpt_policy 行数组，原来没有行时保存空数组。默认核对仍拒绝配额变化；仅 --allow-reconfiguration-append 按日志时间顺序核对完整审计链，第一份 before 必须等于旧完整行，下一份 before 必须等于上一份 after，最后 after 必须等于当前完整行。id、daily_limit、fallback_model_id 始终保持旧配置，原无配额行时只接受默认的无限制及空备用模型配置；只有 day、daily_used、paused、reason、retry_at、updated_at 可沿真实试调用的记录变化。旧日志逐行保持，缺审计、缺一条记录、不连续、字段不完整、模型或 actor 不符、日限及备用模型改变均拒绝核对，不用恢复备份清零实际调用消耗。

收录附件与丢弃记录属于不可再生业务数据。未引用附件的文件随现有上传资源镜像保留，`ingest_assets` 的引用归属与过期时间进入副本并恢复；24 小时清理任务属于 `task_pending_ops`，按已有未完成任务恢复规则续跑。被引用附件的文章文件与本地附件按文件哈希保全。`ingest_discards` 保留规范化链接、原文章 ID、丢弃时间与原因，即使原文章已删除，重建之后仍能阻止再次收录。

获取方式 `entries.acquisition` 写入文章和用户文档表头并保留副本核对值；待补全状态、缺失材料的原地址与原因、提交 Key 的 ID 与名称、提交时间、外部补全尝试状态、附件清单、关联待审条目 ID 与类型参考全部进入副本。操作日志的 Key ID 与名称为调用方身份快照，保留历史名称，不包含认证 Key 或其哈希。原 API Key 的凭据仍按既有规则排除。

## 采集配置与同步日志归档

迁移 `000005` 之前，`./reliquary-admin export-sources` 将两张旧表的非密钥内容写入文件库 `.reliquary/exports/`。文件名为 `sources-000005-<UTC纳秒时间>-<内容SHA256前12位>.json`，JSON 包含 `exported_at`（UTC）、`migration_version: 5`、`source_migration_version`、`tables.data_sources`、`tables.sync_logs` 与 `json_nulls`。仅旧副本重建无法确定源结构版本时可省略 `source_migration_version`。JSON 保存导出时间、目标迁移版本和源结构版本，数据源包括名称、类型、地址、调度、默认内容类型、启用状态及其他配置，同步日志保存历史结果、统计和时间。凭据、密钥、Authorization、Cookie、URL 用户信息与敏感查询参数不进入文件。

`upgrade` 先暂停并完整备份旧数据库，再导出，最后迁移和核对。导出失败时不删表并保持暂停；旧版本完整备份恢复到当前结构时同样先导出再迁移。仅文件库重建读取旧副本时也先归档两表，再恢复其余数据。当前 pack、unpack、rebuild 与 verify 使用删表后的归属清单，不要求已删除的表存在；旧格式的行和原始文件须先严格核对，不能仅忽略旧表丢失。导出的历史 JSON 随文件库和备份保留，文章 `channel` 及来源记录不受删表影响。

完整旧备份恢复到当前版本时，先按原结构核对刚恢复的副本、附件和向量，再导出与迁移；迁移成功后重建索引、刷新当前副本，并再次使用原包的 `snapshot.json` 核对。旧包快照不改写，迁移前核对失败不刷新副本，也不删表。整体退回随包的旧代码与镜像时保留旧结构，继续使用旧包 helper。

迁移 000008 删除原件附件记录、原件路径、保存模式、处理复核字段与空间解析器配置。版本 0 至 7 的离线维护仍按原结构归类这些列；版本 8 起只保存 Markdown 正文与图片。
