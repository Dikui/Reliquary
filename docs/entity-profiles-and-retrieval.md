# 实体维护与资料筛选

实体是个人空间内跨库共用的机构（`organization`）或人物（`person`）。规范名、类型和别名保存在 PostgreSQL，可在资料库的“实体”入口新增、编辑、删除和合并，无需重新部署。业务模型依据 [ADR 0016](adr/0016-store-and-track-event-layer.md)。

## 名录与关联

首次启动新实体功能时，导入参照 AIHOT taxonomy 整理的 18 个机构及机构别名，排除产品与模型名。导入标记单独持久化，重复启动不重复导入，用户删除的预设机构不会自动恢复。后续变化由人工维护。

规范化 key 依次执行 NFKC、去首尾空白、小写、删除全部 Unicode 空白。别名按实体内的规范化 key 去重。不同实体可以具有相同别名，规范化匹配和多重命中的处理由后续事实端点功能使用。

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
