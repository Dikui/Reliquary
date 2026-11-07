# 产品货架变更集

开启货架后，实体、关系、显式资料关联和货架设置通过变更集写入。普通人工维护库沿用原接口。货架的原单项编辑接口会创建一个立即确认的变更集，返回值保持原实体或关系的形状；本机网页记为 `web`，API Key 请求记为 `agent` 并采用 Key 的名称。尚未实现变更集操作的实体物理删除、合并和拆分在货架中拒绝执行。附件上传仍是供属性引用的暂存操作，不代表确认任何货架关联。

HTTP 路径以 `/api/v1` 为前缀，成功结果在 `data` 中。

| 接口 | 行为 |
| --- | --- |
| `POST /libraries/:id/shelf/changesets` | 提交 `source`、`operations`；默认仅保存待确认变更集 |
| `GET /libraries/:id/shelf/changesets` | 按 `status=pending/confirmed`、`page`、`page_size` 分页读取 |
| `GET /libraries/:id/shelf/changesets/:changeset_id` | 读取提议、原值、结果、冲突和已保存决定 |
| `PATCH /libraries/:id/shelf/changesets/:changeset_id` | 提交变更集 `revision` 和部分 `decisions`，只保存进度 |
| `POST /libraries/:id/shelf/changesets/:changeset_id/confirm` | 提交变更集 `revision`，可附决定；重新验证后一次提交 |

来源有三种形式：`{"type":"web"}`、`{"type":"agent","agent_name":"提交方名称"}`、`{"type":"import","import_batch_id":"批次标识","file_date":"2026-11-01T00:00:00Z"}`。导入日期必须明确提供，不以收录时间代替。导入的属性快照采用文件日期；网页和 agent 可以在操作内指定生效时间。来源归属于提议，之后换人确认不改变来源；日志中的 actor 表示实际确认人。

## 操作与决定

每个操作可给出本批唯一的 `id`，省略时由服务端分配。创建提议会预分配实体 ID，保存草稿不会创建该实体。后续操作用 `$操作id` 引用本批前面的实体，或直接使用已有实体 ID。操作按提交顺序验证和执行；同一条目多次修改要按顺序提供递增的修订号。

| `type` | 数据 |
| --- | --- |
| `create_entity` | `entity`，使用现有实体创建字段；可提供 `candidate_entity_id` 请求人工判断是否同一 |
| `update_entity` | `entity_id`、`patch`，`patch.revision` 必填；属性为完整快照 |
| `add_alias` | `entity_id`、`revision`、`alias` |
| `archive_entity` | `entity_id`、`revision`；保留 ID、历史和关联 |
| `missing_entity` | `entity_id`、`revision`；必须决定保留或退市 |
| `save_relation` | `relation`；修改时另带 `relation_id` 和 `relation.version` |
| `end_relation` | `relation_id`、`revision`（关系版本），可带 `valid_to`；保留历史关系 |
| `set_entity_link` | `entry_id`、`entity_id`、`revision`、`excluded`，可带 `relation_type` |
| `configure_shelf` | `library`（已有库设置请求）、`library_updated_at`（读取到的库更新时间） |

决定为 `{item_id, action}`。`apply` 接受提议；`discard` 放弃；`defer` 暂缓。`replace` 附 `replacement` 提交修正后的完整操作，原项 ID 保留。跨类别同名的 `apply` 附 `name_confirmations`，确认具体对象和类别。`same_entity` 附 `target_entity_id`、`target_revision`，只把提议名称及别名加入已有同类别实体，不自动合并提议属性。后续 `$操作id` 引用指向所选的已有实体。

`missing_entity` 的决定只能是 `keep`、`retire`、`discard`、`defer` 或修正提议。在当前基础操作中，`retire` 表示归档该条目；区域可售状态由对应业务操作维护，不能从消失推断区域状态。保留不改变修订号，归档遵守活动子项约束。

所有项都有决定后才能确认。暂缓项在同一事务中转入一个新的待确认变更集，返回 `deferred_changeset_id`。已确认变更集不可再次编辑或重复确认。保存进度也检查变更集修订号，避免两个人覆盖对方的决定。

提交时可带 `confirmed:true`，表示当场确认。没有专门冲突的新建或普通修改可以据此接受；手工值冲突仍须逐项明确 `apply`。跨类别同名、疑似同一和消失必须使用各自的明确决定。

## 差异、冲突和事务

每项包含 `before`、`after`、`operation`、`conflicts`。九类业务冲突为 `new`、`suspected_same`、`cross_kind_same_name`、`modified`、`disappeared`、`unparseable`、`uncertain`、`required_missing`、`cross_table_contradiction`。过期修订另返回 `revision_conflict`。409 响应的 `error.details` 包含重新计算的变更集；失败时已保存的草稿与决定保持原状。

导入修改中的 `manual_before` 标出被改动字段的手工旧值，属性按字段分别追溯来源；另一个属性的导入不会抹掉它的手工来源。旧日志没有来源时保守视为需要保护的人工值。创建与保存草稿均不覆盖主数据。

提交方可用 `issues:[{type,message,details}]` 标出无法解析、含义不确定、必填缺失和跨表矛盾。系统不推断含义，单纯 `apply` 不能跳过这些阻塞项，必须修正、放弃或暂缓。确定的同类别名称、别名和代码仍使用已有唯一性规则；同名候选和明确提供的候选可以由人确认为同一实体，系统不做模糊身份归并。

校验使用真实人工维护写入逻辑，在数据库保存点内试算并完整回滚，包括属性历史、同名确认、日志和图谱队列。保存提议仅持久化变更集。确认时重新验证全部操作、并发修订和库范围，所有写入及暂缓项转移处于同一事务，任一步失败都回滚。确认完成前不执行文件或后台工作进程的外部副作用。

## 来源查询与恢复

`GET /operation-logs` 和 `GET /entities/:entity_id/history` 支持 `source_type`、`agent_name`、`import_batch_id`，可与原筛选及分页组合。每条日志带 `source` 和 `changeset_id`，同名确认和未来属性快照调整也随所属写入保留来源。旧日志的 `source` 为 `{}`。

迁移 43 增加变更集及日志来源，注册到现有导出恢复流程。旧副本只补空表和空来源；旧 JSONL 日志原文继续保留。存在任何变更集或非空日志来源时拒绝降级。创建变更集接口不是反向变更或撤销接口，修正已确认写入需要再提交变更集。
