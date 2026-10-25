# 实体维护与资料筛选

实体是个人空间内跨库共用的机构（`organization`）或人物（`person`）。规范名、类型和别名保存在 PostgreSQL，可在资料库的“实体”入口新增、编辑、删除和合并，无需重新部署。业务模型依据 [ADR 0016](adr/0016-store-and-track-event-layer.md)。

## 名录与关联

首次启动新实体功能时，导入参照 AIHOT taxonomy 整理的 18 个机构及机构别名，排除产品与模型名。导入标记单独持久化，重复启动不重复导入，用户删除的预设机构不会自动恢复。后续变化由人工维护。

规范化 key 依次执行 NFKC、去首尾空白、小写、删除全部 Unicode 空白。别名按实体内的规范化 key 去重。不同实体可以具有相同别名，事实端点的规范化匹配只接受唯一命中。

资料详情的摘要区域可编辑涉及的实体，选择 0–3 个已有机构或人物。保存后返回 `entities_manual=true`，空选择同样记录为人工修改。资料重处理或重建库保留该选择。删除资料仅撤除关联，不删除名录中的实体。

合并时，资料关联、别名与关注转移到保留实体，重复关联和关注去重，保留合并跳转与合并记录，并写入 `merge_entities` 操作日志。删除实体会撤除资料关联和关注，资料正文保留。

资料列表支持按实体筛选，筛选保存在 URL 的 `entity_ids` 中，可以与库、发布时间、星标等条件组合。实体详情显示关联资料及其所属库。

## HTTP 接口

| 操作 | 接口 |
| --- | --- |
| 查询名录 | `GET /api/v1/entities?entity=&entity_type=&limit=50&offset=0` |
| 新增 | `POST /api/v1/entities` |
| 编辑规范名、类型与完整别名列表 | `PATCH /api/v1/entities/:entity_id` |
| 删除 | `DELETE /api/v1/entities/:entity_id` |
| 详情与相关资料 | `GET /api/v1/entities/:entity_id` |
| 合并 | `POST /api/v1/entities/merge` |
| 合并记录 | `GET /api/v1/entities/:entity_id/merge-records` |
| 编辑资料的涉及实体 | `PUT /api/v1/entries/:id/entities` |
| 按实体筛选资料 | `GET /api/v1/entries?entity_ids=id1,id2` |

新增和编辑使用 `canonical_name`、`entity_type`、`aliases`。规范名和单个别名上限 512 个字符。资料实体编辑使用 `{"entity_ids":[]}`；空数组表示明确移除全部实体。

旧的简介编辑、身份模型、开放抽取、合并建议和实体名称向量维护接口及后台任务已移除。资料理解只从已有实体中选择涉及的实体，不创建实体。

## 升级的数据变化

迁移 `000036_redefine_entities` 删除旧实体、旧别名、资料实体关联、实体关注、合并记录与合并建议，移除简介和实体名称向量及其维护记录，清理旧身份模型配置、抽取检查点与实体向量归档字段。资料 ID、正文、图片、来源元数据和星标保留。首次启动后建立新的预设名录。

该迁移不保留旧实体数据。降级仅恢复旧表结构，不能恢复已删除数据；降级还会清除新名录及其关联、关注和人工选择标记。代码升级与备份相互独立，具体备份授权遵循仓库 AGENTS.md。本功能本身不提供操作前最新状态的恢复能力。


## 领域与资料筛选

领域跨库共用，每项为名称和一句定义。在资料页的“领域”页签维护，首次启动导入 Agent 与 LLM，此后增改删均保存在数据库，重启不重复导入。LLM 指大模型本身的架构、训练与后训练、推理能力、模型发布与评测，基于模型构建的应用不算。

HTTP 使用 `GET/POST /api/v1/domains`、`PATCH/DELETE /api/v1/domains/:domain_id` 维护清单。资料详情使用 `PUT /api/v1/entries/:id/domains`，请求为 `{"domain_ids":["id"]}`，允许 0–3 个现有领域；空数组同样记录 `domains_manual=true`。删除领域同时撤除全部资料关联，保留资料正文与人工修改标记。

`GET /api/v1/entries` 的 `entity_ids` 与 `domain_ids` 都支持逗号分隔或重复查询参数。两个条件取交集，同类 ID 内取并集。例如 OpenAI 实体加 Agent 领域只返回同时符合两组条件的资料。批量操作预览与提交沿用该筛选范围。HTTP 检索与 MCP `search` 同样接受 `entity_ids` 和 `domain_ids` 数组，MCP `list_domains` 返回可供筛选的领域 ID、名称与定义。

所有库入库后通过一次资料理解生成中文标题、中文一句话概述，以及各 0–3 个领域和涉及的实体。长文按段处理后合并为整篇结果；模型输出的 ID 不在输入选项中时逐项丢弃。资料的结构化概述仅包含 `overview`，要点和关键名词不再生成或展示。人工编辑的标题、概述、领域及涉及的实体在重处理与来源修订后保留。

领域页的“对已有资料重新判定领域”打开共享批量任务流程，范围为全部库的已有资料。也可通过 `POST /api/v1/entries/batches/preview` 和 `POST /api/v1/entries/batches` 提交 `action=reclassify_domains`，沿用预览指纹与固定名单。后台逐项运行，跳过人工修改过领域的资料，只更新领域，不重建索引或覆盖标题、概述及实体。进度与失败重试沿用批次接口。

调用统计使用 `entry_understanding`，长文合并继续使用 `segment_merge`。迁移 38 仅增加标题和概述的人工标记，并允许新的调用用途；不删除已有资料。回滚会移除新增的人工标记，并把该用途的历史调用映射为原用途，不保留这个用途区分。

## 事实端点与待确认实体

时效型资料理解的 `fact` 可带 `subject_entity_id`、`object_entity_id`，以及无 ID 时的 `subject_type`、`object_type`（`organization`、`person`、`other`）。ID 必须在提供给模型的现有实体选项内，保存前再核对当前目录。无有效 ID 时，端点文字按规范化 key 精确匹配规范名和别名；句中提及和同时命中多个实体都不挂接。不自动增加别名。

未挂接且标为机构或人物的端点按规范化 key 归入待确认实体，记录事实 ID 和主体或对象端。产品、模型等其他类型、仅有署名作者的资料和不产生事实的库不生成候选。每条出现记录对应一个事实的一端，重复处理不增加次数；同一名字出现在事实两端计两次。

实体页下方的待确认列表按出现次数降序排列，可查看事实框架并进入其事件。默认只显示待处理项，可以切换到已忽略项。通过会创建实体；并为别名会为选中的现有实体新增别名。这两种操作仅按该候选的出现记录挂 ID。忽略只改变处理状态，同名新事实继续追加记录，之后仍可通过或并为别名。已经通过或并为别名的同名新端点正常使用实体目录匹配。

在实体编辑器新增别名时，对尚未挂接的历史端点按该别名精确回填一次，已挂接端点保留，多重命中仍不挂接。通过、并为别名、忽略和手动新增别名均写操作日志。现有实体合并同时转移事实端点和已处理候选的目标 ID；删除实体后端点留空，其已处理候选回到待处理状态，出现记录保留。

| 操作 | HTTP 接口 |
| --- | --- |
| 按出现次数查询 | `GET /api/v1/entity-candidates?status=pending&limit=50&offset=0` |
| 候选与出现的事实 | `GET /api/v1/entity-candidates/:id?limit=50&offset=0` |
| 通过为新实体 | `POST /api/v1/entity-candidates/:id/approve` |
| 并为已有实体的别名 | `POST /api/v1/entity-candidates/:id/alias`，请求 `{"entity_id":"目标 ID"}` |
| 忽略 | `POST /api/v1/entity-candidates/:id/ignore` |

列表状态可为 `pending`、`ignored`、`approved`、`aliased` 或 `all`，默认 `pending`。分页 `limit` 为 1–100，`offset` 为非负数。列表 `data` 为 `{items,total}`；详情为 `{candidate,occurrences,total}`，每条出现记录包含 `fact`、`endpoint`、`created_at`。候选包含 `occurrence_count` 与可空的 `entity_id`。事件详情 HTTP 与 MCP 的事实均包含可空的 `subject_entity_id`、`object_entity_id`。


## 人工归组纠正

事件详情页提供移动事实、拆出新事件、合并事实和合并事件。拆出时指定新事件标题；合并时明确选择保留项。事实文字与发生时间采用保留事实的值，其他报道的说法和引文按原资料保留。

| 操作 | HTTP 接口与请求 |
| --- | --- |
| 事实详情与旧事实 ID 解析 | `GET /api/v1/facts/:id` |
| 移动事实 | `POST /api/v1/facts/:id/move`，`{"story_id":"目标事件"}` |
| 拆出新事件 | `POST /api/v1/facts/:id/split`，`{"title":"新事件标题"}` |
| 合并事实 | `POST /api/v1/facts/:id/merge`，`{"keep_fact_id":"保留事实"}` |
| 合并事件 | `POST /api/v1/stories/:id/merge`，`{"keep_story_id":"保留事件"}` |

移动、拆出与事实合并返回规范事实及出处，事件合并返回保留事件详情。旧事实与事件保留 `merged_into_id`，详情接口解析到最终保留项；多次合并继续保留旧链接，重复提交同一合并不重复写日志。事件页同步解析旧事件地址及 `#fact-<ID>` 锚点。自动召回排除已合并的事实记录。

事实合并在事务中转移出处与逐字引文、提及和待确认实体出现记录。出现记录按候选、保留事实和端点去重，保留最早出现时间；当规范化名字唯一对应保留事实的另一端时调整端点，避免倒置描述使后续审批挂错位置；提及与出处重合时保留出处。事件合并转移事实。纠正不创建新事实时间，事件首次与最近时间由保留事实的原创建时间重新计算。

持久保护契约供后续修订和 consolidate 工作复用：

- `facts.manual_corrected=true`：此事实经过人工归组纠正，自动任务不得移动或合并它。
- `stories.manual_corrected=true`：事件归组被人工改变，自动事件合并不得覆盖该决定。
- `entries.grouping_manual=true`：资料的归组经过人工纠正。关联被撤除后也须保留该标记，自动归组不得重新决定其归属。
- 涉及事实的当前出处和提及均设 `entry_facts.manual=true`。通用资料保存不能写回旧的 `grouping_manual`。自动归组提交前再次检查当前资料标记，并在同一空间事务屏障内解析候选的最新事实与事件。

这三个持久标记随详情接口返回；MCP 的事件和资料读取也返回对应标记。四类纠正分别记录 `move_fact`、`split_fact`、`merge_facts`、`merge_stories` 操作日志，记录操作者及前后归属。上述保护契约不包含定期 consolidate 或修订撤证实现。
