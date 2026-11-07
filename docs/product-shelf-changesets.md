# 产品货架变更集

产品地图上传与方案保存见[产品地图导入](product-shelf-import.md)。

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
| `reparse_document` | `entity_id`、文件 `revision`、`reparse:{version_id,parser_key}`，保持同一版本 |
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

## 组成、可售状态与参数

组成使用 `save_relation`，`relation_type` 为 `composed_of`，主体为型号，客体为部件，关系属性 `module_type` 必须是本库配置的模块键。同一模块一个部件为固定组成，多个为选配。单独销售的部件型产品仍创建独立型号，通过组成关系引用一个部件。部件可通过 `create_entity`、`update_entity` 保存 `module_type`、`pack_model`、`cluster_model`、`container_model`；名称和型号写法原样保留。

`set_sale_statuses` 接受 `entity_id`、`revision`、可选 `effective_at` 和 `sale_statuses` 数组，整体替换该型号的区域状态记录。每条记录包含 `region`（区域键）、`status`（GA、DEV、Planning、EOM 或待定）、`planned_eom`、`notes`、`raw` 和 `conditions`。空数组表示明确清空全部记录；缺少某区域表示该区域无记录。空白状态返回 `uncertain` 冲突，提交方须修正为明确状态，或明确选择不写入。系统不自动补待定。导入使用来源中的文件日期作为生效时间。

适用条件写成 `[{"module_type":"module_a","part_ids":["part-a","part-b"]}]`。数组内各模块同时成立，每个模块选择所列部件之一，空条件表示全部适用。模块不能重复，部件集合不能为空，部件必须属于该型号在生效时刻的对应模块。同一区域的多条状态、同一参数的多条值不能覆盖同一组合；重叠时作为含义不确定退回提交方。

参数字段沿用库的 `entity_schema.fields`，通过 `configure_shelf` 定义。参数定义设置 `kinds:["model"]` 和非空 `product_line_ids`（本库产品线 ID 数组），其余类型、单位、选项规则沿用普通属性。字段定义中的产品线可追加，已有值或历史值使用的产品线不能移除。型号必须通过完整层级属于允许的产品线，参数不继承到其他型号。

`set_parameter_values` 接受 `entity_id`、`revision`、可选 `effective_at` 和 `parameter_values`。后者为字段键到值数组的映射，例如：

```json
{
  "id": "energy",
  "type": "set_parameter_values",
  "entity_id": "model-id",
  "revision": 3,
  "parameter_values": {
    "energy": [{"value": {"min": 9, "max": 54}, "conditions": []}]
  }
}
```

数值支持单个数字或含 `min`、`max` 的闭区间，要求下限不大于上限。其他类型的 `value` 使用原字段类型。仅替换请求列出的参数，空值数组明确移除该参数。参数和状态也可随通用属性快照写入，仍受同一校验约束。组合规则字段 `combination_rules` 只接受空数组，保留位置，尚不执行规则。

`GET /api/v1/entities/:entity_id` 对货架型号增加 `shelf`，包含按模块分组的 `composition`、`sale_statuses`、`parameters` 和空 `combination_rules`。组成中的 `optional` 表示有多个部件。可传 `combination` 查询参数，值为 URL 编码的模块键到部件 ID 的 JSON 对象；须选择所有选配模块，固定部件可省略。未指定组合时返回全部记录及条件，指定后只返回条件满足的记录。可用 RFC 3339 `as_of` 同时读取对应时点的属性和组成，早于最早记录时不推断事实。

单卖型号的状态变化在变更集条目的 `warnings` 中返回 `component_sale_status_changed` 和其他使用该部件的型号 ID。提示不阻止确认，也不修改其他型号的状态或修订号。部件自身没有可售状态。若移除组成或调整层级会使已有状态条件或参数失效，须在同一变更集中先修正相关值。

迁移 44 为已开启货架的库补充空字段定义，不改写业务值。既有字段类型或类别与保留字段冲突时拒绝升级。导出、恢复与旧副本核对采用同一补充规则；存在新增业务值、参数定义或相关变更集时拒绝降级。

## 认证记录

标准由 `shelf_config.standards` 配置，每项为 `{key,name,aliases}`。键表示稳定身份，`name` 是规范名，客户写法由提交方明确列入 `aliases`。不按相似拼写或年份推断标准。改名时须把旧规范名保留为别名，已有键和别名继续有效。不同标准不能共享规范名、键或别名。年份单独记在认证记录上。

认证写入使用以下变更集操作，不提供直接写入接口：

| 操作 | 输入与行为 |
| --- | --- |
| `create_certification` | `certification`，可带 `effective_at`。明确新增一条独立认证，同时建立所属节点到认证记录的 `has_certification` 关系。名称由提交方给出。 |
| `update_certification` | `certification`、`revision`，可带 `entity_id` 和 `effective_at`。按识别键更新一条已有记录。`certification` 是完整替换，省略的可选字段表示清空；省略名称表示保留原名。 |

`certification` 包含 `name`（新增必填）、`owner_id`、`standard`、`certification_object`、`cell_part_id`、`region`、`year`、`agency`、`progress`、`start_date`、`obtained_date`、`issued_date`、`validity`、`number`、`cloud_url` 和 `conditions`。`owner_id` 必须明确指向本库的型号、部件、产品名称或平台，不按行名猜测节点。`standard` 接受配置键、规范名或别名，保存为配置键，查询同时返回规范名。`year` 是可选整数；三个日期使用 `YYYY-MM-DD`；有效期保留原写法；云盘链接只接受 HTTP(S)。认证对象、地区、机构和进度均保存提交方写法，不在代码里固定客户值。未指定地区或电芯部件时用空值参与识别，不补填推测值。

识别键为所属节点、标准、认证对象、电芯部件和地区，不包含年份、机构、进度或证书编号。更新匹配零条或多条时返回 `uncertain` 阻塞冲突，不会自动新增、合并或拆分。显式 `entity_id` 也必须与识别键一致。多条候选须由人工在 `apply` 决定中提供 `target_entity_id` 与 `target_revision`，明确选中一条；零条时须修正操作或明确改为新增。新增操作本身表示提交方明确建立独立记录，因此可保存同键的不同认证，后续自动匹配会报告多条候选。

认证对象可以填写 BMS；无法确定所属节点的独立认证行返回 `uncertain`，即使普通接受也不能写入。提交方可以修正、放弃或暂缓。模块不根据认证对象名称自动创建。电芯部件和条件引用须属于所属节点的组成范围，不能引用其他库的部件。条件沿用模块与部件集合的合取写法，须由所属范围内同一个型号同时满足。结构调整不能使已有或未来生效认证的条件、部件或归属关系失效。

`GET /api/v1/libraries/:id/shelf/certifications` 返回本库认证，可用 `standard`（配置键、规范名或别名）筛选，并可用 RFC 3339 `as_of` 读取历史。未知标准返回空列表。型号、部件、产品名称和平台详情增加 `shelf.certifications`，每项包含认证实体、完整字段、`standard_name` 和 `origin`。`origin` 为 `self`、`component`（来自部件）或 `ancestor`（来自上层）。仅含一个部件的型号把该部件认证显示为自身认证，底层记录仍只有一份。

型号详情沿用 `combination` 与 `as_of`。指定组合时同时检查条件和电芯部件，未指定时保留可满足的认证及原条件；上层认证不出现在无法满足其部件范围的型号上。记录的历史、修订号、导入文件日期、人工值保护和日志来源均沿用变更集与属性快照规则。

迁移 45 只给旧货架增加空认证字段定义，不生成认证或标准。字段冲突时拒绝升级。旧副本导入与核对使用相同默认字段补充规则。存在标准配置、认证属性或历史、相关变更集时拒绝降级。

## 资料文件与版本

原件先通过 `POST /libraries/:id/entity-attachments` 的 multipart `file` 字段暂存。上传不会建立货架归属，未确认的原件可沿用附件删除接口清理。归类只通过 `add_document_version` 变更集操作确认，`document` 示例：

```json
{
  "id": "manual",
  "type": "add_document_version",
  "document": {
    "name": "客户原始文件名称",
    "owner_id": "model-id",
    "requirement_id": "requirement-id",
    "attachment_id": "attachment-id",
    "version_label": "Rev A",
    "stage": "final",
    "origin": "提交方提供的来源",
    "occurred_at": "2026-11-01T00:00:00Z",
    "conditions": []
  }
}
```

`owner_id` 指向本库活动的型号、产品名称、平台或部件。`requirement_id` 可空，非空时必须是本库活动资料要求。原件和 HTTP(S) `cloud_url` 至少提供一项，也可同时提供；云盘链接不触发外部下载。`stage` 必填，使用 `draft` 或 `final`。名称和版本号原样保存，缺少版本号时按该文件的上传顺序编号。新文件需要显式名称，后续版本保留文件名称。`origin` 保存提交方写法，操作来源另由变更集的 `source` 保存；时间优先 `occurred_at`，未给时使用导入文件日期或确认时刻。

同节点同资料要求默认匹配原文件。没有资料要求的文件每次默认新建，追加版本须提供操作的 `entity_id` 和读取到的 `revision`。系统在提议中固定所选文件及修订号，确认时检查并发变化；内部修订值 `-1` 表示该提议预期新建。当前版本替换在确认事务中完成，旧版标为 `is_current:false`，原件和元数据保留。通用实体操作不能创建资料文件或更改其类别，通用关系操作不能更改版本的归属或资料要求。

证书报告的 `owner_id` 指向认证记录，同时提供与认证记录一致的 `certificate_number`。仅提供编号时，必须在本库活动认证记录中唯一匹配；不匹配或重复编号返回 `uncertain`，提交方须修正或明确指定认证记录。编号只用于确定认证归属，不用于判断两份文件是否为同一文件；没有资料要求时，追加版本仍须明确指定已有文件。不根据文件名称猜归属。

版本可带 `entry_id` 指向通过现有收录接口保存的本库文本资料，每个版本使用独立资料。确认后显式关联到资料文件与所属节点，不按正文名称匹配。无显式文本且有本机原件时，确认后按库配置选择解析器并在后台解析；未配置解析器或选择不解析时返回 `parse_status:"not_parsed"`。解析配置、状态与重解析操作见[货架资料解析](product-shelf-parsers.md)。有显式文本时返回 `completed`，表示已有文本，不代表完成后续模型处理。适用条件沿用模块与部件的合取写法，并校验所属节点范围。

`GET /entities/:entity_id/document-versions` 按顺序号倒序返回全部版本，包含当前标记、来源、时间、文本引用与归类信息。`GET /entities/:entity_id/document-versions/:version_id/download` 下载指定版本的本机原件；仅有云盘链接的版本返回 404，可使用版本中的链接。版本引用的附件不能删除，包括历史版本。新增版本沿用 `update_entity` 操作日志类型，以 `document_version` 保存版本详情，并保留变更集来源。

通过 `configure_shelf` 的 `library.shelf_config.max_file_bytes` 配置单文件字节上限，省略或 0 使用 32 MiB 默认值，正数自定义上限。普通人工维护库仍使用 32 MiB。上传按实际字节检查，归类确认时再次检查最新限制，调整上限不会改写已有版本或原件。

迁移 46 新增版本表并注册现有导出、完整备份和恢复校验，原件继续保存在已有本机附件存储目录。旧副本升级只补空版本表。存在资料版本、相关变更集或文件大小配置时拒绝降级，避免遗失业务数据。当前版本检索过滤由后续能力实现。

## 资料要求与阶段

资料要求仍使用 `create_entity`、`update_entity`，类别为 `document_requirement`，完整字段快照放在 `attributes.requirement`。确认前只产生提议；网页单项编辑沿用自动确认的单操作变更集。示例：

```json
{
  "id": "manual",
  "type": "create_entity",
  "entity": {
    "name": "提交方给定的资料名称",
    "kind": "document_requirement",
    "attributes": {
      "requirement": {
        "name_zh": "中文原名",
        "name_en": "Original English name",
        "file_category": "提交方文件类别",
        "stage_label": "原表阶段标注",
        "draft_due_stage": "configured_draft_stage",
        "final_due_stage": "configured_final_stage",
        "markets": {"configured_region": "required"},
        "owner": "提交方负责人",
        "priority": "提交方优先级",
        "source_list": "来源清单",
        "source_version": "客户版本"
      }
    }
  }
}
```

`final_due_stage` 必填，`draft_due_stage` 可省略。阶段使用本库配置键，初稿不能晚于终稿。`stage_label` 只展示原表写法，不参与到期判断。市场键来自本库区域配置，值为 `required`（必需）、`optional`（可选）或 `not_needed`（不需要）；未提供的市场保留未知，不代填。其他文字原样保存。

适用范围通过 `save_relation` 的 `applies_to` 关系保存：

- 产品线要求：`subject_id` 为资料要求，`object_id` 为产品线，不填模块属性。
- 模块要求：模块类型属于库配置，不新增模块实体；`subject_id` 与 `object_id` 均为该资料要求，关系属性为 `{"module_types":["configured_module"]}`，可包含多个不同的配置键。该自引用只用于货架资料要求的模块适用范围。

同一要求可以同时绑定产品线与模块，计算清单会去重。未完成资料要求字段的实体不能新建适用关系。已有资料要求的完整字段不能清空以隐藏义务，须明确归档。清单更新使用已有变更集差异流程：新增项创建，修改项带修订号更新，消失项用 `missing_entity` 提议并由人选择 `retire` 后归档；也可明确使用 `archive_entity`。消失本身不归档、不删除文件，归档后所有型号按当前活动要求计算。

产品名称与型号用 `attributes.current_stage` 保存配置阶段键，仍走 `update_entity` 并检查修订号。型号没有该字段时沿用产品名称；移除型号字段恢复继承。两层都未填写时只显示文件有无。完整属性快照及 `effective_at`、导入文件日期沿用现有时间线规则。
