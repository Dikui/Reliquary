# 数据归属与文件库恢复格式

所有 PostgreSQL 非系统普通表及其每列均显式分类：public 表在 `internal/maintenance/rules.go`，新登记表在 `internal/maintenance/registry.go` 并合入同一归属清单，扩展拥有的表在 `internal/maintenance/extension_rules.go`。目录核对也遍历分区表、外部表和物化视图，出现未经审核的关系类型立即停止。维护导出遇到未分类 schema、表或列立即报错；测试也检测重复分类、漏列和拼写错误。新增结构必须同时更新分类与逐表恢复测试。最新完整清单见下节逐列审计表，历史对照表保留原有 public 表的详细归属，结构版本 `000005` 已删除 `data_sources` 与 `sync_logs`。之后新增的表与列按结构版本在文末补充，当前完整清单以 `Rules` 和登记框架的目录核对结果为准。


## 最新结构逐列归属审计

本表来自结构版本 22 的真实 PostgreSQL 目录，共 101 张非系统表、1125 列。列由 `maintenance audit` 自动核查，表格每格完整列出该归属的列，没有使用通配符。副本列包括统一表头、非密钥配置及高成本处理结果；仅备份列由脱敏 PostgreSQL dump 保留。凭据列归入可再生，但必须通过重新登录或配置生成新值，不恢复原值；审计的 recovery 字段标记 replace_secret。凭据不放入备份。扩展可再生列只在扩展版本、默认行数和指纹匹配时成立。

```bash
./reliquary-admin maintenance audit
```

| 表 | 副本列 | 仅备份列 | 可再生列 | 可再生凭据列（重新配置） |
| --- | --- | --- | --- | --- |
| `paradedb._typmod_cache` | 无 | 无 | `id`、`typmod` | 无 |
| `pgivm.pg_ivm_immv` | 无 | 无 | `immvrelid`、`viewdef`、`ispopulated`、`lastivmupdate` | 无 |
| `public.api_keys` | `id`、`space_id`、`name`、`last_used_at`、`expires_at`、`revoked_at`、`created_at`、`updated_at` | 无 | 无 | `key_hash`、`api_key` |
| `public.article_embeddings` | 无 | 无 | `entry_id`、`space_id`、`library_id`、`model_id`、`dimension`、`embedding`、`content_hash`、`created_at`、`updated_at` | 无 |
| `public.auth_tokens` | `id`、`user_id`、`token_type`、`expires_at`、`is_revoked`、`created_at`、`updated_at` | 无 | 无 | `token` |
| `public.chatgpt_accounts` | `id`、`client_id`、`issuer`、`subject`、`email`、`scopes`、`expires_at`、`earliest_refresh_at`、`status`、`last_error`、`welcome_acknowledged`、`created_at`、`updated_at` | 无 | 无 | `credentials` |
| `public.chatgpt_hosts` | `id`、`ext_agent_host_id`、`created_at` | 无 | 无 | 无 |
| `public.chatgpt_login_attempts` | `callback_url`、`client_id`、`account_id`、`expires_at` | 无 | 无 | `state`、`nonce`、`verifier` |
| `public.chatgpt_policy` | `id`、`daily_limit`、`daily_used`、`day`、`paused`、`reason`、`retry_at`、`fallback_model_id`、`updated_at` | 无 | 无 | 无 |
| `public.chunks` | 无 | 无 | `id`、`space_id`、`library_id`、`entry_id`、`content`、`chunk_index`、`is_enabled`、`start_at`、`end_at`、`pre_chunk_id`、`next_chunk_id`、`chunk_type`、`parent_chunk_id`、`image_info`、`relation_chunks`、`indirect_relation_chunks`、`created_at`、`updated_at`、`deleted_at`、`metadata`、`status`、`content_hash`、`flags`、`seq_id`、`video_info`、`context_header`、`search_title`、`source_locations` | 无 |
| `public.embedding_rebuild_items` | `id`、`run_id`、`kind`、`source_id`、`attachment_path`、`status`、`attempts`、`error`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.embedding_rebuild_runs` | `id`、`space_id`、`status`、`model_id`、`model_name`、`dimension`、`profile_hash`、`total_count`、`completed_count`、`failed_count`、`error`、`attempt_id`、`lease_until`、`created_at`、`updated_at`、`started_at`、`finished_at`、`candidate`、`switched_at` | 无 | 无 | 无 |
| `public.embedding_rebuild_vectors` | 无 | 无 | `run_id`、`kind`、`source_id`、`attachment_path`、`entry_id`、`library_id`、`model_id`、`dimension`、`embedding`、`content_hash` | 无 |
| `public.embedding_vector_archives` | 无 | 无 | `run_id`、`space_id`、`payload`、`created_at` | 无 |
| `public.embeddings` | 无 | 无 | `id`、`created_at`、`updated_at`、`source_id`、`source_type`、`chunk_id`、`entry_id`、`library_id`、`model_id`、`dimension`、`embedding`、`is_enabled` | 无 |
| `public.entities` | `id`、`space_id`、`canonical_name`、`normalized_name`、`entity_type`、`description`、`merged_into_id`、`created_at`、`updated_at` | 无 | `name_embedding`、`embedding_model_id`、`embedding_dimension`、`embedding_needs_rebuild` | 无 |
| `public.entity_aliases` | `id`、`space_id`、`entity_type`、`entity_id`、`alias`、`normalized_alias`、`resolution_method`、`created_at` | 无 | 无 | 无 |
| `public.entity_embedding_rebuild_runs` | `id`、`space_id`、`status`、`reason`、`error`、`model_id`、`dimension`、`profile_hash`、`processed_count`、`total_count`、`model_calls`、`attempts`、`attempt_id`、`created_at`、`updated_at`、`started_at`、`finished_at` | 无 | 无 | 无 |
| `public.entity_merge_records` | `id`、`space_id`、`keep_entity_id`、`merge_entity_id`、`actor`、`created_at` | 无 | 无 | 无 |
| `public.entity_merge_suggestions` | `id`、`space_id`、`new_entity_id`、`candidate_entity_id`、`entry_id`、`reason`、`status`、`created_at`、`decided_at` | 无 | 无 | 无 |
| `public.entries` | `id`、`space_id`、`library_id`、`type`、`title`、`description`、`content_type`、`source_unverified`、`source_kind`、`vault_path`、`vault_body`、`starred`、`collect_reason`、`agent_summary`、`agent_tags`、`source_name`、`structured_summary`、`source`、`parse_status`、`enable_status`、`file_name`、`file_type`、`file_size`、`file_path`、`file_hash`、`normalized_url`、`content_hash`、`content_sim_hash`、`content_rune_count`、`storage_size`、`metadata`、`created_at`、`updated_at`、`processed_at`、`error_message`、`deleted_at`、`summary_status`、`channel`、`pending_subtasks_count`、`custom_metadata`、`follow_ready_at`、`content_type_corrected`、`acquisition`、`incomplete`、`external_completion_attempted`、`ingest_api_key_id`、`ingest_api_key_name`、`last_ingest_api_key_id`、`last_ingest_api_key_name`、`missing_materials`、`content_type_hint`、`related_entry_id`、`source_ref`、`source_body_hash`、`identifier`、`graph` | 无 | `search_seq_id`、`search_text`、`search_published_at`、`embedding_model_id` | 无 |
| `public.entry_entities` | `space_id`、`entry_id`、`entity_id`、`created_at` | 无 | 无 | 无 |
| `public.entry_processing_outputs` | `entry_id`、`space_id`、`library_id`、`run_id`、`body_hash`、`content_type`、`relations`、`images`、`updated_at` | 无 | 无 | 无 |
| `public.entry_processing_spans` | `id`、`entry_id`、`attempt`、`span_id`、`parent_span_id`、`name`、`kind`、`status`、`input`、`output`、`metadata`、`error_code`、`error_message`、`error_detail`、`started_at`、`finished_at`、`duration_ms`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.entry_repost_sources` | `id`、`space_id`、`library_id`、`entry_id`、`original_url`、`normalized_url`、`source_fingerprint`、`author`、`publisher`、`published_at`、`published_at_missing`、`match_method`、`sim_hash_distance`、`created_at` | 无 | 无 | 无 |
| `public.entry_segment_runs` | `id`、`space_id`、`entry_id`、`phase`、`plan_hash`、`body_hash`、`plan`、`result`、`status`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.entry_topics` | `space_id`、`entry_id`、`topic_id`、`source`、`is_historical`、`created_at` | 无 | 无 | 无 |
| `public.evaluation_answers` | `question_id`、`entry_id`、`grade` | 无 | 无 | 无 |
| `public.evaluation_questions` | `id`、`space_id`、`set_id`、`method`、`request`、`source`、`status`、`source_record_id`、`suggested_results`、`answer_deleted`、`revision`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.evaluation_run_results` | `run_id`、`question_id`、`question`、`snapshot`、`results`、`metrics`、`hits`、`error` | 无 | 无 | 无 |
| `public.evaluation_runs` | `id`、`space_id`、`set_id`、`set_snapshot`、`options`、`snapshot`、`status`、`summary`、`error`、`created_at`、`finished_at` | 无 | 无 | 无 |
| `public.evaluation_sets` | `id`、`space_id`、`name`、`description`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.follow_feed_library_states` | `space_id`、`library_id`、`last_viewed_at` | 无 | 无 | 无 |
| `public.follow_feed_states` | `space_id`、`last_viewed_at` | 无 | 无 | 无 |
| `public.follows` | `space_id`、`target_type`、`target_id`、`created_at` | 无 | `entity_id`、`topic_id` | 无 |
| `public.graph_pending_sync` | 无 | 无 | `id`、`space_id`、`entity_id`、`relation_id`、`relation_version`、`deleted`、`created_at`、`completed_at` | 无 |
| `public.image_embeddings` | 无 | 无 | `entry_id`、`space_id`、`library_id`、`attachment_path`、`model_id`、`dimension`、`embedding`、`content_hash`、`created_at`、`updated_at` | 无 |
| `public.ingest_assets` | `id`、`space_id`、`name`、`file_type`、`size`、`path`、`created_at`、`referenced_at`、`entry_id` | 无 | 无 | 无 |
| `public.ingest_discards` | `space_id`、`normalized_url`、`entry_id`、`discarded_at`、`reason`、`library_id`、`fingerprint` | 无 | 无 | 无 |
| `public.libraries` | `model_tiers`、`id`、`name`、`description`、`space_id`、`chunking_config`、`embedding_model_id`、`summary_model_id`、`created_at`、`updated_at`、`deleted_at`、`type`、`indexing_strategy`、`creator_id`、`system_role`、`template`、`directory`、`review_criteria`、`source_evidence_rule`、`feed_enabled` | 无 | 无 | 无 |
| `public.library_content_types` | `library_id`、`name`、`position`、`enabled`、`extract_graph` | 无 | 无 | 无 |
| `public.library_relation_types` | `library_id`、`name`、`position`、`enabled` | 无 | 无 | 无 |
| `public.maintenance_state` | `backup_at`、`backup_status`、`backup_path`、`backup_size_bytes`、`backup_error`、`drill_at`、`drill_status`、`drill_error`、`backup_schedule`、`drill_schedule`、`schedule_timezone`、`backup_directory`、`automatic_enabled` | 无 | `id`、`paused`、`pause_mode`、`pause_reason`、`paused_at`、`updated_at`、`backup_due_at`、`drill_due_at` | 无 |
| `public.model_calls` | 无 | `id`、`space_id`、`requested_at`、`model_id`、`model_name`、`provider`、`purpose`、`entry_id`、`input_tokens`、`output_tokens`、`input_price`、`output_price`、`currency`、`cost`、`cost_status`、`duration_ms`、`success`、`error_category`、`attempt`、`retry_number`、`processing_attempt`、`span_id` | 无 | 无 |
| `public.models` | `id`、`space_id`、`name`、`display_name`、`type`、`source`、`description`、`parameters`、`is_default`、`status`、`created_at`、`updated_at`、`deleted_at`、`is_builtin`、`managed_by` | 无 | 无 | 无 |
| `public.operation_logs` | `id`、`space_id`、`actor`、`operation_type`、`object_id`、`before_value`、`after_value`、`created_at`、`api_key_id`、`api_key_name` | 无 | 无 | 无 |
| `public.relation_sources` | `relation_id`、`entry_id`、`evidence`、`run_id`、`body_hash` | 无 | 无 | 无 |
| `public.relation_suppressions` | `space_id`、`library_id`、`subject_id`、`relation_type`、`object_id`、`actor`、`created_at` | 无 | 无 | 无 |
| `public.relation_type_merges` | `library_id`、`from_type`、`to_type`、`created_at` | 无 | 无 | 无 |
| `public.relations` | `id`、`space_id`、`library_id`、`subject_id`、`relation_type`、`object_id`、`version`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.resource_access_grants` | `id`、`resource_id`、`access_scope`、`expires_at`、`revoked_at`、`created_at` | 无 | 无 | `token_hash` |
| `public.resource_bindings` | `id`、`resource_id`、`space_id`、`owner_type`、`owner_id`、`relation`、`created_at` | 无 | 无 | 无 |
| `public.resources` | `id`、`handle`、`space_id`、`provider`、`physical_path`、`location_hash`、`kind`、`mime_type`、`original_name`、`size`、`content_hash`、`lifecycle`、`expires_at`、`state`、`created_at`、`updated_at`、`deleted_at` | 无 | 无 | 无 |
| `public.schema_migrations` | 无 | 无 | `version`、`dirty` | 无 |
| `public.search_records` | 无 | `id`、`space_id`、`created_at`、`source`、`method`、`query`、`libraries`、`request`、`results`、`duration_ms`、`fallback_fulltext`、`reranked`、`evaluation_run_id`、`error` | 无 | 无 |
| `public.spaces` | `id`、`name`、`description`、`status`、`business`、`storage_used`、`created_at`、`updated_at`、`deleted_at`、`retrieval_config`、`embedding_model_id` | 无 | 无 | `credentials` |
| `public.spatial_ref_sys` | 无 | 无 | `srid`、`auth_name`、`auth_srid`、`srtext`、`proj4text` | 无 |
| `public.system_settings` | `id`、`key`、`value`、`value_type`、`category`、`description`、`is_secret`、`requires_restart`、`last_modified_by`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.task_dead_letters` | `id`、`space_id`、`task_type`、`scope`、`scope_id`、`related_id`、`payload`、`last_error`、`fail_count`、`failed_at` | 无 | 无 | 无 |
| `public.task_pending_ops` | `id`、`task_type`、`queue`、`payload`、`headers`、`state`、`max_retry`、`retried`、`fail_count`、`timeout_ns`、`retention_ns`、`deadline`、`group_name`、`created_at`、`updated_at`、`process_at`、`started_at`、`completed_at`、`last_failed_at`、`last_error`、`dispatched_at`、`object_id`、`generation`、`delivery`、`worker_pool`、`cancel_requested` | 无 | 无 | 无 |
| `public.task_queue_counters` | `queue`、`day`、`processed`、`failed` | 无 | 无 | 无 |
| `public.topic_backfill_batches` | `id`、`job_id`、`ordinal`、`entry_ids`、`entry_count`、`status`、`attempts`、`error`、`created_at`、`updated_at`、`retry_at` | 无 | 无 | 无 |
| `public.topic_backfill_jobs` | `id`、`space_id`、`topic_id`、`topic_name`、`topic_description`、`status`、`total`、`processed`、`failed_entries`、`failed_batches`、`created_at`、`updated_at` | 无 | 无 | 无 |
| `public.topics` | `id`、`space_id`、`name`、`description`、`created_at`、`updated_at`、`library_ids` | 无 | 无 | 无 |
| `public.users` | `id`、`username`、`email`、`password_hash`、`avatar`、`space_id`、`is_active`、`created_at`、`updated_at`、`deleted_at` | 无 | 无 | 无 |
| `tiger.addr` | 无 | 无 | `gid`、`tlid`、`fromhn`、`tohn`、`side`、`zip`、`plus4`、`fromtyp`、`totyp`、`fromarmid`、`toarmid`、`arid`、`mtfcc`、`statefp` | 无 |
| `tiger.addrfeat` | 无 | 无 | `gid`、`tlid`、`statefp`、`aridl`、`aridr`、`linearid`、`fullname`、`lfromhn`、`ltohn`、`rfromhn`、`rtohn`、`zipl`、`zipr`、`edge_mtfcc`、`parityl`、`parityr`、`plus4l`、`plus4r`、`lfromtyp`、`ltotyp`、`rfromtyp`、`rtotyp`、`offsetl`、`offsetr`、`the_geom` | 无 |
| `tiger.bg` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tractce`、`blkgrpce`、`bg_id`、`namelsad`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 |
| `tiger.county` | 无 | 无 | `gid`、`statefp`、`countyfp`、`countyns`、`cntyidfp`、`name`、`namelsad`、`lsad`、`classfp`、`mtfcc`、`csafp`、`cbsafp`、`metdivfp`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 |
| `tiger.county_lookup` | 无 | 无 | `st_code`、`state`、`co_code`、`name` | 无 |
| `tiger.countysub_lookup` | 无 | 无 | `st_code`、`state`、`co_code`、`county`、`cs_code`、`name` | 无 |
| `tiger.cousub` | 无 | 无 | `gid`、`statefp`、`countyfp`、`cousubfp`、`cousubns`、`cosbidfp`、`name`、`namelsad`、`lsad`、`classfp`、`mtfcc`、`cnectafp`、`nectafp`、`nctadvfp`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 |
| `tiger.direction_lookup` | 无 | 无 | `name`、`abbrev` | 无 |
| `tiger.edges` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tlid`、`tfidl`、`tfidr`、`mtfcc`、`fullname`、`smid`、`lfromadd`、`ltoadd`、`rfromadd`、`rtoadd`、`zipl`、`zipr`、`featcat`、`hydroflg`、`railflg`、`roadflg`、`olfflg`、`passflg`、`divroad`、`exttyp`、`ttyp`、`deckedroad`、`artpath`、`persist`、`gcseflg`、`offsetl`、`offsetr`、`tnidf`、`tnidt`、`the_geom` | 无 |
| `tiger.faces` | 无 | 无 | `gid`、`tfid`、`statefp00`、`countyfp00`、`tractce00`、`blkgrpce00`、`blockce00`、`cousubfp00`、`submcdfp00`、`conctyfp00`、`placefp00`、`aiannhfp00`、`aiannhce00`、`comptyp00`、`trsubfp00`、`trsubce00`、`anrcfp00`、`elsdlea00`、`scsdlea00`、`unsdlea00`、`uace00`、`cd108fp`、`sldust00`、`sldlst00`、`vtdst00`、`zcta5ce00`、`tazce00`、`ugace00`、`puma5ce00`、`statefp`、`countyfp`、`tractce`、`blkgrpce`、`blockce`、`cousubfp`、`submcdfp`、`conctyfp`、`placefp`、`aiannhfp`、`aiannhce`、`comptyp`、`trsubfp`、`trsubce`、`anrcfp`、`ttractce`、`tblkgpce`、`elsdlea`、`scsdlea`、`unsdlea`、`uace`、`cd111fp`、`sldust`、`sldlst`、`vtdst`、`zcta5ce`、`tazce`、`ugace`、`puma5ce`、`csafp`、`cbsafp`、`metdivfp`、`cnectafp`、`nectafp`、`nctadvfp`、`lwflag`、`offset`、`atotal`、`intptlat`、`intptlon`、`the_geom`、`tractce20`、`blkgrpce20`、`blockce20`、`countyfp20`、`statefp20` | 无 |
| `tiger.featnames` | 无 | 无 | `gid`、`tlid`、`fullname`、`name`、`predirabrv`、`pretypabrv`、`prequalabr`、`sufdirabrv`、`suftypabrv`、`sufqualabr`、`predir`、`pretyp`、`prequal`、`sufdir`、`suftyp`、`sufqual`、`linearid`、`mtfcc`、`paflag`、`statefp` | 无 |
| `tiger.geocode_settings` | 无 | 无 | `name`、`setting`、`unit`、`category`、`short_desc` | 无 |
| `tiger.geocode_settings_default` | 无 | 无 | `name`、`setting`、`unit`、`category`、`short_desc` | 无 |
| `tiger.loader_lookuptables` | 无 | 无 | `process_order`、`lookup_name`、`table_name`、`single_mode`、`load`、`level_county`、`level_state`、`level_nation`、`post_load_process`、`single_geom_mode`、`insert_mode`、`pre_load_process`、`columns_exclude`、`website_root_override` | 无 |
| `tiger.loader_platform` | 无 | 无 | `os`、`declare_sect`、`pgbin`、`wget`、`unzip_command`、`psql`、`path_sep`、`loader`、`environ_set_command`、`county_process_command` | 无 |
| `tiger.loader_variables` | 无 | 无 | `tiger_year`、`website_root`、`staging_fold`、`data_schema`、`staging_schema` | 无 |
| `tiger.pagc_gaz` | 无 | 无 | `id`、`seq`、`word`、`stdword`、`token`、`is_custom` | 无 |
| `tiger.pagc_lex` | 无 | 无 | `id`、`seq`、`word`、`stdword`、`token`、`is_custom` | 无 |
| `tiger.pagc_rules` | 无 | 无 | `id`、`rule`、`is_custom` | 无 |
| `tiger.place` | 无 | 无 | `gid`、`statefp`、`placefp`、`placens`、`plcidfp`、`name`、`namelsad`、`lsad`、`classfp`、`cpi`、`pcicbsa`、`pcinecta`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 |
| `tiger.place_lookup` | 无 | 无 | `st_code`、`state`、`pl_code`、`name` | 无 |
| `tiger.secondary_unit_lookup` | 无 | 无 | `name`、`abbrev` | 无 |
| `tiger.state` | 无 | 无 | `gid`、`region`、`division`、`statefp`、`statens`、`stusps`、`name`、`lsad`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 |
| `tiger.state_lookup` | 无 | 无 | `st_code`、`name`、`abbrev`、`statefp` | 无 |
| `tiger.street_type_lookup` | 无 | 无 | `name`、`abbrev`、`is_hw` | 无 |
| `tiger.tabblock` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tractce`、`blockce`、`tabblock_id`、`name`、`mtfcc`、`ur`、`uace`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 |
| `tiger.tabblock20` | 无 | 无 | `statefp`、`countyfp`、`tractce`、`blockce`、`geoid`、`name`、`mtfcc`、`ur`、`uace`、`uatype`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom`、`housing`、`pop` | 无 |
| `tiger.tract` | 无 | 无 | `gid`、`statefp`、`countyfp`、`tractce`、`tract_id`、`name`、`namelsad`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`the_geom` | 无 |
| `tiger.zcta5` | 无 | 无 | `gid`、`statefp`、`zcta5ce`、`classfp`、`mtfcc`、`funcstat`、`aland`、`awater`、`intptlat`、`intptlon`、`partflg`、`the_geom` | 无 |
| `tiger.zip_lookup` | 无 | 无 | `zip`、`st_code`、`state`、`co_code`、`county`、`cs_code`、`cousub`、`pl_code`、`place`、`cnt` | 无 |
| `tiger.zip_lookup_all` | 无 | 无 | `zip`、`st_code`、`state`、`co_code`、`county`、`cs_code`、`cousub`、`pl_code`、`place`、`cnt` | 无 |
| `tiger.zip_lookup_base` | 无 | 无 | `zip`、`state`、`county`、`city`、`statefp` | 无 |
| `tiger.zip_state` | 无 | 无 | `zip`、`stusps`、`statefp` | 无 |
| `tiger.zip_state_loc` | 无 | 无 | `zip`、`stusps`、`statefp`、`place` | 无 |
| `topology.layer` | 无 | 无 | `topology_id`、`layer_id`、`schema_name`、`table_name`、`feature_column`、`feature_type`、`level`、`child_id` | 无 |
| `topology.topology` | 无 | 无 | `id`、`name`、`srid`、`precision`、`hasz`、`useslargeids` | 无 |

`search_records` 与 `model_calls` 的全部列只有 PostgreSQL 备份，没有副本。`graph_pending_sync` 是重建通知，关系正本与来源仍进副本。`relations` 的来源计数由 `relation_sources` 按资料去重计算，Neo4j 中保存的 `source_count` 属于图谱副本，不是独立正本。本次没有改变业务列的存储归属；修正文档中旧基线表格被误当成完整清单、仅备份表沿用 Sidecar 分类但未明确呈现的歧义。

## 历史格式与基线对照

维护副本与核对清单的 `format_version` 为 6，表名使用 `spaces`、`api_keys`、`libraries`、`entries` 和 `entry_` 关联表，行外层字段使用 `space_id`、`library_id`、`entry_id`、`entry_ids`。完整数据库备份的底层导入及核对接受版本 1 至 5，当前文件库重建入口的版本限制见本文末节：版本 1 先映射个人空间表及归属列，其中版本 1 和 2 将 `knowledges`、四张 `knowledge_` 关联表和行外层 `knowledge_id`、`knowledge_ids` 映射为当前名字。版本 1 至 3 的 `knowledge_bases` 表名与行外层 `knowledge_base_id` 分别映射为 `libraries`、`library_id`。先验证原文件校验和，再在原版本命名下比较规范行哈希，任意嵌套 JSON 不递归改名。旧表头仅在旧副本重建入口转换。旧报告 metadata 中已定义的关联资料列表与任务 payload 外层资料键在导入时转换；日志的 before/after、任意用户嵌套 JSON 与设置键保持原值。只追加的 `operation_logs.jsonl` 保留旧外层归属键，SQL 列与网页 HTTP 字段使用新名字。下一次刷新副本写入版本 6，完成清单原子替换后才移除旧表文件。标题与摘要向量 `article_embeddings` 包含文章和导入的用户文档，不包含整理结果；表名保留，外键列为 `entry_id`。

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
| `follow_feed_library_states` | 无 | `space_id`、`library_id`、`last_viewed_at` | 无 | 无 |
| `follow_feed_states` | 无 | `space_id`、`last_viewed_at` | 无 | 无 |
| `follows` | 无 | `space_id`、`target_type`、`target_id`、`created_at` | `entity_id`、`topic_id` | 无 |
| `image_embeddings` | 无 | 无 | `entry_id`、`space_id`、`library_id`、`attachment_path`、`model_id`、`dimension`、`embedding`、`content_hash`、`created_at`、`updated_at` | 无 |
| `ingest_assets` | 无 | `id`、`space_id`、`name`、`file_type`、`size`、`path`、`created_at`、`referenced_at`、`entry_id` | 无 | 无 |
| `ingest_discards` | 无 | `space_id`、`normalized_url`、`entry_id`、`discarded_at`、`reason` | 无 | 无 |
| `libraries` | 无 | 无 | 无 | `model_tiers`、`id`、`name`、`description`、`space_id`、`chunking_config`、`embedding_model_id`、`summary_model_id`、`created_at`、`updated_at`、`deleted_at`、`type`、`indexing_strategy`、`creator_id` |
| `entry_entities` | 无 | `space_id`、`entry_id`、`entity_id`、`created_at` | 无 | 无 |
| `entry_processing_spans` | 无 | `id`、`entry_id`、`attempt`、`span_id`、`parent_span_id`、`name`、`kind`、`status`、`input`、`output`、`metadata`、`error_code`、`error_message`、`error_detail`、`started_at`、`finished_at`、`duration_ms`、`created_at`、`updated_at` | 无 | 无 |
| `entry_repost_sources` | 无 | `id`、`space_id`、`library_id`、`entry_id`、`original_url`、`normalized_url`、`source_fingerprint`、`author`、`publisher`、`published_at`、`published_at_missing`、`match_method`、`sim_hash_distance`、`created_at` | 无 | 无 |
| `entry_topics` | 无 | `space_id`、`entry_id`、`topic_id`、`source`、`is_historical`、`created_at` | 无 | 无 |
| `entries` | `title`、`content_type`、`source_unverified`、`starred`、`collect_reason`、`structured_summary`、`created_at`、`acquisition` | `id`、`space_id`、`library_id`、`type`、`description`、`source_kind`、`vault_path`、`vault_body`、`agent_summary`、`agent_tags`、`source_name`、`source`、`parse_status`、`enable_status`、`file_name`、`file_type`、`file_size`、`file_path`、`file_hash`、`storage_size`、`metadata`、`updated_at`、`processed_at`、`error_message`、`deleted_at`、`summary_status`、`channel`、`pending_subtasks_count`、`custom_metadata`、`follow_ready_at`、`content_type_corrected`、`normalized_url`、`content_hash`、`content_sim_hash`、`content_rune_count`、`incomplete`、`external_completion_attempted`、`ingest_api_key_id`、`ingest_api_key_name`、`last_ingest_api_key_id`、`last_ingest_api_key_name`、`missing_materials`、`content_type_hint`、`related_entry_id` | `search_seq_id`、`search_text`、`search_published_at`、`embedding_model_id` | 无 |
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
| `topic_backfill_batches` | 无 | `id`、`job_id`、`ordinal`、`entry_ids`、`entry_count`、`status`、`attempts`、`error`、`created_at`、`updated_at`、`retry_at` | 无 | 无 |
| `topic_backfill_jobs` | 无 | `id`、`space_id`、`topic_id`、`topic_name`、`topic_description`、`status`、`total`、`processed`、`failed_entries`、`failed_batches`、`created_at`、`updated_at` | 无 | 无 |
| `topics` | 无 | `id`、`space_id`、`name`、`description`、`library_ids`、`created_at`、`updated_at` | 无 | 无 |
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

`entries.vault_body` 保存各库的完整正文。已发布普通资料的维护副本继续从资料 Markdown 读取正文；自有资料和 AI 报告的正文还保留在数据库与维护副本中，并与资料文件核对。未发布、待审和处理中的内容尚无文件时，在副本保留数据库已有原文及收录 metadata。分块不是正文恢复来源。图片由文件库及完整备份保护。文件库重建使用统一表头和按库目录，未登记的手放文件不会自动收录。

账号密码哈希选择保留在 `config/users.json`，使仅用文件库重建后仍能用原密码登录。密码明文、登录 JWT、API Key、API Key 哈希、资源访问令牌哈希、ChatGPT 令牌和 OAuth 临时 state/nonce/verifier 均不输出。会话与临时访问记录的非密钥元信息全部保留，旧认证值不恢复。当前账号与模型配置保留非凭据部分，旧结构的解析器字段按历史版本规则读取；任意嵌套 JSON 中的凭据键、Authorization/Cookie、URL 用户信息和敏感 query 参数递归剔除。headers 与 custom_headers 只保留 Accept、Content-Type、User-Agent 三种明确公开的头，其他头即使自定义名称不含 token 也列为待填凭据。当前数据库的密文与解密后密钥、SYSTEM_AES_KEY、JWT_SECRET、数据库密码等只在内存中形成核对集合；副本的非密钥字符串中出现同一长凭据时，把对应字串替换为 [credential excluded]，保留其他文字。Basic 或 Bearer 开头的普通文章原文不会仅因开头词语而清空。ChatGPT 凭据中的 token_type、scope、username、email、client_id 等公开元信息不当作秘密字串，避免误删普通内容。被剔除字段写入待配置清单，不保留其值。迁移后模型需要配置、ChatGPT 账号需要重新登录，完成配置后再恢复对应功能。

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

变化通过 GORM 写入回调触发去抖导出，默认与表头相同为 5 秒；定期读取已提交的 PostgreSQL 一致快照捕捉原始 SQL 和队列更新。单文件临时写入、fsync、读回验证、原子替换，内容一致不改 inode。快照 manifest 最后替换，另存根目录除 .reliquary 以外的各库资料与图片文件哈希。导入先检查副本和全部原始文件哈希、表清单及持久表的依赖恢复覆盖，有中断、遗漏或篡改时在写入数据库前停止，使用 `verify --repair` 从数据库补齐。

核对逐表比较非派生列的规范 JSON、行数及 JSONB null 类型。JSONB 字面值 null 与 SQL NULL 单独记录，导入后逐列保持原类型，操作日志中的空 before_value 不会误变成 SQL NULL。模型和 ChatGPT 账号的安全行与待填凭据 ID 进入核对清单，原始 status 保留供逐行核对。未剔除凭据的模型，status 与 updated_at 精确比较，任何改变都会失败。只有旧清单已列出的凭据受影响 ID 可以由原 status 转为 models.needs_configuration，再恢复清单记录的原 status；该行 updated_at 允许凭据操作改变。其他 status 不忽略。模型 parameters.extra_config 中 maintenance_required_fields、maintenance_original_status、maintenance_original_updated_at 是恢复提醒，只排除这些明确键，其他参数精确核对。ChatGPT 待重新登录账号允许 needs_login 和完成新登录的 connected，expires_at、earliest_refresh_at 与 updated_at 仅在上述待填账号行允许变化；last_error 仍精确核对。版本 4 及以前的订阅源净化与暂停规则仅用于旧备份读取及删表前导出，当前维护不恢复采集功能。

待处理操作是任务权威，随数据库备份与格式 5 的维护副本保存。启动时 active 或 dispatched 操作恢复为 pending，每次启动恢复增加投递代次 delivery，并清空 dispatched_at、worker_pool，更新 updated_at。离线 `queue-replay` 在 snapshot 暂停且没有消费者时按页重推全部到期操作，未来的 process_at 继续等待。核对允许同 ID 未完成行的 pending 或 dispatched 恢复转换及递增的投递代次；payload、generation、object_id、重试次数、截止时间、process_at 和完成历史均精确比较。可运行操作的投递元数据可以重建，cancelled、archived、completed 的全部字段保持精确核对。Redis 数据不进入备份，恢复后从未完成操作补推。版本 9 以前的离线维护仍按原 task_jobs 列归属处理，升级 000009 时删除旧任务；不导入旧队列消息。维护暂停标记与定时 due_at 可重新生成，最近备份和恢复演练的时间、状态、目录、大小及错误属于不可再生历史，进入副本并恢复；只有 backup_directory 是目标机器可调整的配置路径。全文索引、分块、向量和搜索 sequence ID 可重新生成，向量值与检索分数不做字节比较。固定检索使用真实 ParadeDB BM25 查询 maintenancearchive、observations、reference，核对排序后的文章 ID，忽略分数。文件仍按字节哈希核对，副本 config 文件由其规范数据库内容核对。正常恢复先与旧清单严格核对，再开放凭据填写；填写过程可以追加新的操作日志，完成凭据配置前再次按旧清单核对。该步骤显式使用 --allow-reconfiguration-append；只允许追加日志的场合使用 --allow-operation-log-append。旧日志按 ID 保留逐行完整内容与 JSONB null 类型，只接受新的日志 ID。--allow-model-change 显式允许库 embedding_model_id 和 model_tiers.embedding 变更，以及旧、新选中 embedding 模型的名称、展示名、说明、来源、参数和 updated_at 变化；原有模型的 ID、租户、类型和创建时间、其他未选模型及全部不可再生业务表仍严格比较。新加模型只允许新选中的 Embedding 行，不允许删掉旧模型。新模型的全部向量生成完整后再开放使用。后续日常 verify 检查当前数据库与副本一致。

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

获取方式 `entries.acquisition` 写入文章和用户文档表头并保留副本核对值；缺少图片状态、缺失材料的原地址与原因、提交 Key 的 ID 与名称、提交时间、外部补全尝试状态、附件清单、关联待审条目 ID 与类型参考全部进入副本。操作日志的 Key ID 与名称为调用方身份快照，保留历史名称，不包含认证 Key 或其哈希。原 API Key 的凭据仍按既有规则排除。

## 采集配置与同步日志归档

迁移 `000005` 之前，`./reliquary-admin export-sources` 将两张旧表的非密钥内容写入文件库 `.reliquary/exports/`。文件名为 `sources-000005-<UTC纳秒时间>-<内容SHA256前12位>.json`，JSON 包含 `exported_at`（UTC）、`migration_version: 5`、`source_migration_version`、`tables.data_sources`、`tables.sync_logs` 与 `json_nulls`。仅旧副本重建无法确定源结构版本时可省略 `source_migration_version`。JSON 保存导出时间、目标迁移版本和源结构版本，数据源包括名称、类型、地址、调度、默认内容类型、启用状态及其他配置，同步日志保存历史结果、统计和时间。凭据、密钥、Authorization、Cookie、URL 用户信息与敏感查询参数不进入文件。

`upgrade` 先暂停并完整备份旧数据库，再导出，最后迁移和核对。导出失败时不删表并保持暂停；旧版本完整备份恢复到当前结构时同样先导出再迁移。历史版本的文件库重建会先归档两表，再恢复其余数据；当前入口拒绝未登记的旧副本，见本文末节。当前 pack、unpack、rebuild 与 verify 使用删表后的归属清单，不要求已删除的表存在；旧格式的行和原始文件须先严格核对，不能仅忽略旧表丢失。导出的历史 JSON 随文件库和备份保留，文章 `channel` 及来源记录不受删表影响。

完整旧备份恢复到当前版本时，先按原结构核对刚恢复的副本、附件和向量，再导出与迁移；迁移成功后重建索引、刷新当前副本，并再次使用原包的 `snapshot.json` 核对。旧包快照不改写，迁移前核对失败不刷新副本，也不删表。整体退回随包的旧代码与镜像时保留旧结构，继续使用旧包 helper。

迁移 000008 删除原件附件记录、原件路径、保存模式、处理复核字段与空间解析器配置。版本 0 至 7 的离线维护仍按原结构归类这些列；版本 8 起只保存 Markdown 正文与图片。

### 五库配置（迁移 000010）

`libraries` 的 `system_role`、`template`、`directory`、`review_criteria`、`source_evidence_rule`、`feed_enabled` 属于配置正本。`library_content_types` 的 `library_id name position enabled extract_graph`，`library_relation_types` 的 `library_id name position enabled` 同样属于配置，必须完整备份与恢复。子表在库之后恢复。

`spaces.embedding_model_id` 是空间共享向量模型的配置正本。旧 `libraries.embedding_model_id` 和 `model_tiers.embedding` 保留用于旧副本兼容，运行读取投影为空间选择，不能作为各库独立的模型选择。模型设置的旧 HTTP 入口写入空间值。维护核对允许向量配置变更时一并检查空间选择；重建配置副本包含空间和两个库配置子表。

版本 10 之前的维护使用原有列归属，不要求新列或新表。迁移 000010 将原库保留为机构发布，新增其余系统库，按原来源类别迁移资料及索引归属；原正文、文件路径和业务字段保留。开发用 down 将资料归回原库，生产退回仍必须恢复升级前完整备份。

### 收录契约列（结构版本 11）

`entries.source_ref`、`source_body_hash`、`identifier`、`graph` 均属于 sidecar 不可再生业务数据。`source_body_hash` 保留首次提交正文的规范化指纹，不因图片补全而变化。`has_update`、`latest_entry_id` 为按库和来源查询的派生响应，不是数据库列。版本 10 及以前没有这四列；恢复旧副本时使用空字符串与 NULL 默认值，核对时保留对其他字段的严格比较。

版本 12 的 `ingest_discards.library_id` 与 `ingest_discards.fingerprint` 属于 sidecar 持久数据。版本 11 及以前仍按原列清单核对。旧副本缺少两列时，按资料原库及链接指纹投影；无法确定原库的历史记录保存在独立历史范围，不影响当前库收录。原有行、原因、时间和链接均保留。

000014 新增归属：`chunks.source_locations`、`embedding_rebuild_vectors` 全列、`embedding_vector_archives` 全列为 Regenerated；`embedding_rebuild_runs.candidate`、`switched_at` 为 Sidecar 操作状态。空间的图片向量开关位于既有 Configuration 字段 `retrieval_config` 内。候选向量、旧向量归档仅存 PostgreSQL，可从正文重新生成；运行表及明细表通过 `PersistentTables` 登记，两个运行字段随副本严格恢复；两张向量表通过 `RegeneratedTables` 登记但不导出其行。旧快照仅接受迁移 000014 的确定默认值，其他修改仍被拒绝。恢复后按最终正文重建位置；候选运行的可再生向量丢失后须重新建立候选集，不能直接切换。
## 按库文件与统一表头

所有系统库和自建库都使用 `libraries.directory`，路径为 `<英文目录>/<标题短名>-<ID前8位>.md`。不设年月子目录，避免以不完整的发布时间决定路径，同时减少阅读时的层级。标题短名沿用最多 80 个 Unicode 字符、过滤文件系统特殊字符和空白改连字符的规则。资料的库与目录不可变。图片统一为 `attachments/<资料ID>/image-<内容哈希前16位>.<扩展名>`，正文使用相对路径；此目录只接收图片。

每个库完整保存调用方提交的 Markdown 正文，表头不是正文的一部分。表头和正文均由数据库单向生成，在 Obsidian 手改不会回写数据库；刷新、核对修复或文件重建会覆盖修改。AI 报告也使用数据库正文恢复副本。星标的 false、显式 graph 的 false 是有意义的值，会输出；其他空字符串、空列表、空摘要以及无标记的 source_unverified/has_update 不输出。

以下是顶层字段的固定顺序。键名固定为英文，含义不随库变化。

| 顺序 | 字段 | 含义及输出条件 |
| --- | --- | --- |
| 1 | `id` | 资料 UUID |
| 2 | `library_id` | 稳定的所属库 ID |
| 3 | `library` | 所属库显示名称 |
| 4 | `title` | 标题 |
| 5 | `url` | 有值时输出原文链接 |
| 6 | `publisher` | 有值时输出发布者 |
| 7 | `author` | 有值时输出作者，即使与发布者相同 |
| 8 | `published_at` | 有值时输出发布时间，不用收录时间代替 |
| 9 | `collected_at` | UTC 收录时间 |
| 10 | `content_type` | 所属库的内容类型，可为自定义值 |
| 11 | `entities` | 非空实体链接列表，排序去重 |
| 12 | `topics` | 非空主题列表，排序去重 |
| 13 | `summary` | 非空结构摘要，内部按 overview、points、key_terms 排序，空项省略 |
| 14 | `starred` | 星标布尔值 |
| 15 | `collect_reason` | 有值时输出收录理由 |
| 16 | `acquisition` | 有值时输出获取方式 |
| 17 | `source_ref` | 自有型来源标识 |
| 18 | `identifier` | 外部长效型标识 |
| 19 | `source_unverified` | 仅 true，表示来源未核实 |
| 20 | `has_update` | 仅 true，表示有更新的记录 |
| 21 | `latest_entry_id` | 有 source_ref 时的最新记录 ID，沿用现有接口键名 |
| 22 | `graph` | 调用方明确设置时输出 true 或 false |
| 23 | `related_entry_ids` | 非空关联资料 ID 列表，排序去重 |

各库示例见 [ikb-07 运行存证](tasks/ikb-07/evidence/runtime-headers.md)。所有示例都遵循同一字段表，无值不补 null。表头没有正文位置或向量字段。

## 新持久数据的登记入口

`internal/maintenance/registry.go` 的 `PersistentTables` 是新增可移植持久表的入口。一个登记项同时声明 `Columns` 逐列归属、`File` 副本文件、`After` 恢复依赖和 `Since` 首次出现的数据库结构版本。新表只需添加此登记项及对应 SQL 迁移，不再分别修改导出表名单、导入顺序或核对名单。

示例结构如下，示例表没有在本任务创建：

```go
"future_relations": {
    Columns: Rule{Sidecar: "id source_id target_id created_at"},
    File: "data/future_relations.json",
    After: []string{"entities", "entries"},
    Since: 13,
},
```

`File` 使用 `data/<表名>.json` 或 `config/<表名>.json`，一表一文件。必须列全主键、外键、业务列和历史时间列；只有确定可重算的列放在 Regenerated。敏感列沿用 SecretColumns 与净化机制。After 声明所有必须先恢复的父表；缺依赖或依赖环在写数据库前失败。登记项从 Since 指定版本进入列归属核对，新列未归类仍直接失败。已有登记项增加列时仍按数据库迁移规范维护历史版本映射。

第一个登记对象为库配置：`libraries` 使用既有 `config/libraries.json`，`library_content_types` 与 `library_relation_types` 保留既有 `data/<表名>.json` 文件位置，保留完整数据库备份底层的旧映射。两个子表虽位于 data/，列归属仍为 Configuration。库在空间及账号之后恢复，子表在库之后恢复。库的名称、目录、模板、审核规则、内容类型、关系类型都参与逐列核对。

副本新增 `registry.json`，自身格式版本为 1；主 manifest 的 `registry_version` 为 1，文件哈希在主 manifest 中登记。原副本主格式继续为 5，没有改变原有表文件格式。文件库重建拒绝没有受支持登记版本的旧副本，包括没有 registry_version 的旧格式 1 至 5，并提示使用匹配版本读取原文，再通过当前收录接口重新导入。不会猜测旧目录对应哪个库。底层完整数据库备份读取仍保留原有名称、默认值和结构版本映射，独立于当前文件库重建入口。没有生产数据迁移或旧目录扫描兼容。旧 schema 的维护 helper 仅导出副本并按原哈希核对，不生成库目录或改写旧表头；旧文件修复需使用匹配版本。

关系正本、来源、抑制记录和关系类型合并已从结构版本 18 按上述方式登记；测评集、题目、答案、运行和逐题结果已按结构版本 21 沿用该入口。检索记录与模型调用统计只随 PostgreSQL 完整备份保存，不进 `.reliquary/`；两者已在结构版本 21 与 22 逐列登记到 `BackupOnlyTables`。Redis 临时队列和派生索引仍按现有规则处理。

`./reliquary-admin maintenance rebuild-files` 从现有数据库重新生成所有库的 Markdown 和维护副本；它覆盖手改表头及正文，不调用模型。删除所有库的资料目录后可以逐文件重建；应保留 `attachments/` 中不可重算的图片，图片或整个磁盘丢失须从包含文件库的完整备份恢复。`./reliquary-admin rebuild <文件库>` 仍用于从文件库和副本恢复空数据库，导入前核对原文件，恢复后逐列核对原副本。`verify --repair` 修复数据库拥有的表头及正文。


### 向量运行与结构版本

候选运行及其明细分别登记在 `data/embedding_rebuild_runs.json` 和 `data/embedding_rebuild_items.json`，按空间、模型、运行的依赖顺序恢复。`candidate` 与 `switched_at` 与其他运行字段一样参与严格比较。`RegeneratedTables` 登记两张结构版本 14 的派生向量表；副本不保存其行，但目录核对仍拒绝未知表及未知列。

副本 manifest 的可选 `schema_version` 记录来源结构版本，防止对当前配置重复施加历史默认值。版本 14 升级仅允许迁移定义的本地默认模型、空空间模型选择和运行字段默认值；模型参数或已有配置的任意其他变化仍拒绝。恢复正文分块时重新计算位置；`rebuild-files` 只重建文件，不改变已保存的分块位置。

000015 的主题适用库和分库查看时间均属于 Sidecar。分库检查点经 `PersistentTables` 登记；原空间检查点保留。升级及旧副本导入按空间为每个已有库复制旧检查点，主题默认适用全部库；其他原有字段及行必须完全保留。

## 长文处理检查点

版本 16 的 `entry_segment_runs` 全列属于 sidecar，写入 `.reliquary/data/entry_segment_runs.json`，恢复顺序在空间与资料之后。范围与哈希、逐段完成状态及模型结果、合并结果共同决定续跑位置，不能只保留“已完成”标记。该表进入完整备份与文件库副本，删除资料时级联删除。

副本格式保持 5；结构版本低于 16 的维护核对只允许该新增表为空。当前版本检查点被修改、丢失或替换为空计划均不通过核对。

`entry_processing_outputs` 从结构版本 17 登记为 sidecar，副本路径为 `data/entry_processing_outputs.json`。这里的关系是处理证据，由关系正本消费；图片说明及其位置、不可读原因同样保存。恢复时从该结果重建 `image_description` 分块和向量，不需要重新调用视觉模型。库的关系类型自动补充写入已有配置正本，操作日志同时记录系统处理行为。


## PostgreSQL 关系正本与待同步记录

结构版本 18 新增五张表，副本格式仍为 5，登记格式仍为 1。关系以库加主体、类型、客体唯一，空间实体跨库共用，各库的来源及人工抑制分别保存。`relation_sources` 以关系加资料唯一，`evidence` 保留位置列表和每份证据的发言人。来源数量通过来源行计算，不保存冗余计数。

| 表 | 全部列的归属 | 副本路径 | 恢复依赖 |
| --- | --- | --- | --- |
| `relations` | Sidecar: id space_id library_id subject_id relation_type object_id version created_at updated_at | `data/relations.json` | spaces libraries entities library_relation_types |
| `relation_sources` | Sidecar: relation_id entry_id evidence run_id body_hash | `data/relation_sources.json` | relations entries |
| `relation_suppressions` | Sidecar: space_id library_id subject_id relation_type object_id actor created_at | `data/relation_suppressions.json` | spaces libraries entities library_relation_types |
| `relation_type_merges` | Configuration: library_id from_type to_type created_at | `data/relation_type_merges.json` | library_relation_types |
| `graph_pending_sync` | Regenerated: id space_id entity_id relation_id relation_version deleted created_at completed_at | 不进入副本 | 恢复后由图谱全量重建生成投影 |

既有 `library_relation_types` 保持 Configuration 与 `data/library_relation_types.json` 登记。旧副本升级核对只投影四张新持久表的空默认值；当前非空关系、来源、版本、抑制及合并记录被改动或丢失会核对失败。待同步记录不参与数据相等核对，不进入文件库实时副本；PostgreSQL 完整备份仍包含该表。完整恢复及仅文件库恢复在解除暂停前从 PG 正本重建 Neo4j 并严格核对。

## 导入顺序与恢复核对

导出在 PostgreSQL repeatable read 一致快照中读取所有归属表，文件名与 JSON 键稳定，manifest 最后原子替换。导出文件没有消费顺序；恢复先核验全部文件，再按 `replicaImportOrder` 和登记框架的拓扑排序写入同一事务。`TestIKB20ImportOrderCoversActualForeignKeys` 从最新数据库 `pg_constraint` 读取每条外键，验证所有副本表都在顺序中且父表先于子表。缺依赖、环或遗漏表在写入前失败。

关系正本在 spaces、users、libraries、library_relation_types、entities 之后，relation_sources 还依赖 entries；抑制记录与类型合并依赖实体及库关系类型。evaluation_sets 在 spaces 之后，questions 在 sets 之后，answers 在 questions 之后，runs 在 spaces 之后，run_results 在 runs 之后。标准答案允许保留已删资料 ID，因此没有 entry 外键。历史来源检索记录 ID 也不要求 search_records 随文件库恢复。

核对覆盖所有不可再生字段、行数、JSONB null 类型、正文与表头、attachments、待审资源、模型与库配置、分块向量数量、模型及维度、关系及来源行、测评题和标准答案。副本差异及快照差异输出具体表和主键；复合键包含 relation_id 与 entry_id 或 question_id 与 entry_id。正文损坏输出文件路径。文件库恢复向量暂缺时输出 pending_vectors，恢复继续执行向量重建，`verify --vectors` 全部通过后解除暂停。

```bash
./reliquary-admin verify --vectors
./reliquary-admin maintenance graph-verify
```

`graph-verify` 只核对，不自动修复。它比较全部实体节点及关系边的 ID、端点、类型、库、版本与来源计数，差异包含 PG 表名及行 ID。日常 `graph-check` 继续核对并修复。完整恢复在最终核对之前从 PG 重建图谱、刷新维护副本，新增系统重建日志允许追加，已有日志仍必须保留。

恢复演练使用临时 PG 数据库、独立临时 Redis 容器中的 DB 0、受限 Neo4j 命名空间和临时文件目录。恢复真实待处理操作的 Redis 唤醒，但只消费独立队列的探针，不触发模型或外部工具。探针验证暂停期间零执行、解除暂停后一次完成；实际业务任务的完成由完整恢复集成测试验证。结果 JSON 包含原件核对、Redis 补推与暂停探针、Neo4j 重建及最终核对。结束清理临时数据库、Redis、图谱命名空间与文件；清理失败保留 ownership，下一次 schedule run 清理后重试。

结构版本 23 将 `entries.related_entry_id`、`ingest_discards.entry_id`、`topic_backfill_batches.entry_count` 与 `topic_backfill_jobs.failed_entries` 归为 Sidecar；版本 22 及以前仍按原列名称核对。副本格式 6 写入新行字段，格式 1 至 5 的原文件先校验再转换。

## 模型调用内容归属，结构版本 24

`model_call_contents` 的 `id`、`request_gzip`、`response_gzip`、`request_bytes`、`response_bytes`、`response_type`、`http_status`、`complete` 全部属于仅备份证据。`model_calls.processing_attempt` 与 `model_calls.span_id` 同样仅备份。`model_call_content_deletions.space_id` 和 `entry_id` 保存删除标记，也属于仅备份操作证据。模型实际响应不能可靠再生；请求包含当次完整提示词、正文和模型参数。它们随 PG dump 备份与恢复，不进入 `.reliquary/`，仅文件库恢复不能找回调用内容。

`BackupOnlyTables` 登记全部新列，版本 24 之前使用旧规则；审计遇到未知列即失败。内容表以调用 ID 为主键及账本外键，账本删除时级联。资料删除时由触发器清理其内容并写删除标记，历史统计继续保留。内容写入与资料删除使用相同的事务级 advisory lock，已排队但延迟写入的内容不会在删除之后重新出现。gzip 字节参与完整备份的逐行哈希核对，清单不复制内容。旧版本升级只接受新增空内容表及默认关联字段，原账本仍逐行核对。副本格式保持 6。

默认内容永不清理。`RELIQUARY_MODEL_CALL_CONTENT_RETENTION_DAYS` 和 `RELIQUARY_MODEL_CALL_CONTENT_RETENTION_ATTEMPTS` 分别控制保留天数和每篇资料最近 N 个处理尝试，0 或未配置表示不限；同时配置时两项均生效。非资料调用不受尝试数量限制。旧的 `RELIQUARY_MODEL_CALL_RETENTION_DAYS` 仍控制整个统计账本，删除账本也会级联清理内容。

`RELIQUARY_MODEL_CALL_CAPTURE_EMBEDDING` 与 `RELIQUARY_MODEL_CALL_CAPTURE_RERANK` 默认开启，设置 `false` 关闭对应内容捕获，统计照常。正常向量响应只保存数量及维度，错误响应保留原文。图片二进制用 SHA-256 与附件路径引用替换，正文文字、提示词和所有其他响应字节完整保留。请求头不保存，已知凭据及 JSON 凭据字段脱敏。持久化由最多 64 项的进程队列异步执行，超时、失败或队列饱和仅记日志；正常停止会刷新队列。
