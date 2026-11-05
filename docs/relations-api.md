# 关系正本 HTTP 接口

资料标识统一为 `entry_id`，资料列表使用 `entry_ids`，不接受旧字段别名。
接口位于 `/api/v1`，网页不带凭据，外部工具使用 API Key 认证，范围为个人空间。图谱检索的 HTTP 与 MCP 用法见 [图谱检索](search-api.md#图谱检索)。关系管理操作使用本文 HTTP 接口。

- `GET /entities/:entity_id/relations?library_id=<库 ID>`：可选库过滤，未传时返回全部库；按 `relation_type` 分组。每条含关系 ID、全局主体与客体 ID、库 ID 与名称、版本、方向 `incoming/outgoing`、对端 `peer`、`source_count` 和 `sources`。每个来源含资料 ID、标题、运行 ID、正文哈希与 `evidence`；每份证据含 `locations` 和可选 `speaker`，结构沿用处理输出。已合并实体 ID 自动定位到目标实体。成功返回 `{"success":true,"data":[]}`，无关系时为空数组。
- `DELETE /relations/:relation_id`：删除关系和所有来源，保存所属库的三元组抑制与 `delete_relation` 日志。同一事务写入两端实体的待同步记录。不存在或不属于空间时返回 404；成功返回 `{"success":true}`。
- `POST /libraries/:id/relation-types/merge`：请求 `{"from_type":"合作","to_type":"投资"}`。两种类型须在库内，合并现有关系与来源、改写抑制、禁用旧类型并保存持久映射及 `merge_relation_types` 日志。之后抽出的旧名称仍沿映射链定位到最终类型。重复提交已完成的同一合并幂等；已合并的来源类型不能另改目标。成功返回 `{"success":true}`。

位置以保存后的 Markdown 正文 Unicode 字符范围为坐标，包含原有标题路径、段落序号与正文哈希。一个来源可以有多个发言人与位置，来源数量仍按资料计数。按实体列关系使用同一 PostgreSQL 只读快照读取版本、来源及数量。

## 人工维护库

`entity_mode=managed` 时关系由网页或 HTTP 维护，可以没有文档来源。资料删除或重新处理不会删除这些关系。该模式使用下述专用合并、拆分接口；禁用关系类型合并及旧的实体名称/简介单独修改接口。

- `POST /libraries/:id/relations`：`{subject_id,relation_type,object_id,attributes,valid_from?}`，返回创建的关系及 version。可选 `valid_from` 为 RFC 3339 业务生效时间，省略时使用保存时刻，可补录过去时间，不允许未来时间或零时间。
- `PATCH /relations/:relation_id`：相同字段加当前 `version`，在新记录的 `valid_from` 处结束旧记录，并返回新关系 ID、版本和有效区间。省略 `valid_from` 时使用保存时刻；新时间必须晚于旧记录的 `valid_from`，保证旧区间非空。
- `DELETE /relations/:relation_id?version=N&valid_to=<RFC3339>`：必须传当前版本，可选查询参数 `valid_to` 指定业务结束时间，省略时使用保存时刻；必须晚于该记录的 `valid_from` 且不晚于现在。不生成自动抽取抑制记录。

两端必须为本库活动实体。库关系类型可设 `hierarchical`、`subject_kinds`、`object_kinds` 和属性定义 `fields`，两端类别数组为空表示不限。层级边由子项指向父项，子项类别的 parent_kinds 必须允许父项类别；允许多种层级类型，每种类型内在任意时刻分别禁止多父项及成环。补录会检查已有关系（包括已结束记录）的有效区间；首尾相接的区间不重叠，只有同一时刻生效的路径才构成环。三元组的活动唯一索引保持不变。仍有活动关系的关系类型不能停用；只剩已结束关系时可以停用，但字段定义仍受历史值约束，层级标志不能改。关系类型的 `fields`、`subject_kinds`、`object_kinds` 未配置时返回 `[]`，实体 `auto_match` 未配置时返回 `{}`。`GET /entities/:entity_id/relations?as_of=<RFC3339>` 可查看指定时刻的关系，省略时返回当前关系。有效区间为 `[valid_from, valid_to)`，须满足 `valid_from < valid_to`，空结束时间表示未结束。图谱隐藏已结束或归档端点的关系，恢复实体后可重新显示。修改在同一事务写操作日志与图谱同步通知，记录时间仍为实际保存时刻；合并、拆分等结构调整仍立即执行。

## 自动抽取库的人工动作与副本一致性

修改内容类型使用 `PUT /api/v1/entries/:id/content-type`，请求 `{"content_type":"所属库启用类型"}`。按资料 graph 覆盖和类型默认值决定补抽或撤除关系。实体合并使用 `POST /api/v1/entities/merge`，请求 `{"keep_entity_id":"保留ID","merge_entity_id":"被合并ID"}`。删除资料使用 `DELETE /api/v1/entries/:id`，同时撤除该资料的关系来源，最后一个来源删除后关系也删除。显式更新正文和图片后重新处理会替换该资料的证据，不累加重复来源。

这些动作与 PG 正本及待同步通知在同一事务提交。Neo4j 异步读取 PG 当前状态；查询返回的 `pending_sync` 可提示副本落后。人工删除留下库内抑制，重新处理不能复活该三元组。同库同来源覆盖保留原资料 ID，同步撤除旧派生数据并在后台重建图谱；不保留旧正文版本。维护核对见 [个人使用指南](personal-use.md#neo4j-图谱查询副本)。

## 产品属性、文献关联与结构调整

- `GET /libraries/:id/entity-conversion`：返回文献数、按存储类型分组的实体数、关系数。`PUT /libraries/:id` 可带 `entity_mode: "managed"`、`entity_schema`、`entity_kind_mapping`，将已有实体类型映射到库内类别并保留原数据。
- `POST /libraries/:id/entities`、`PATCH /entities/:entity_id`：属性使用 `attributes`；可选 `effective_at` 为 RFC 3339 时间。PATCH 必须传当前 `revision`。属性是完整快照，不是字段增量；未改属性时省略 attributes。相同快照不重复写入；修改字段同步到生效时间晚于本次 effective_at 的全部已有快照（包括已生效的），其他字段保持原值。
- `GET /entities/:entity_id/attributes?at=...`：返回该时刻的 `attributes`、`effective_at`、`revision` 与 `known`。省略 at 查询现在；仅日期按本地时区零点解释，跨时区调用建议发送含时区的完整时间；早于最早已知快照时 known=false。
- `GET /entities/:entity_id/attribute-history?page=1&page_size=20`：分页返回全部生效快照，包括未来快照。`GET /entities/:entity_id/history` 仍返回操作日志。
- `POST /libraries/:id/entity-attachments`：multipart 的 `file` 字段上传附件，最多 32 MiB。返回附件 ID、名称、类型和大小。`GET /entity-attachments/:attachment_id` 读取元数据；追加 `/download` 下载。属性中的附件值为该 ID，必须与实体或关系在同一库。`DELETE /entity-attachments/:attachment_id` 仅删除无当前或历史引用的附件。
- `GET /entries/:id/entity-links`：仅查看文献与人工维护库产品的关联。`PUT /entries/:id/entity-links/:entity_id` 接受 `excluded` 与可选 `relation_type`。文献可属于其他库，产品必须在同一空间的人工维护库。省略类型保留旧值，新关联默认为 references。
- 文献库的 `product_library_ids` 配置自动匹配范围；产品库的 `document_relation_types` 配置关联类型，必须保留 references。自动匹配不推断规格书、测试报告等语义类型。
- `POST /entities/:entity_id/merge`：目标 ID 在路径中，请求 `{revision,source_id,source_revision,attributes,effective_at?,drop_relation_ids?,document_relation_preference?}`。必须显式给出合并后的完整属性。关联类型冲突可用 document_relation_preference 指定 source 或 target；相反的排除决定仍拒绝。
- `POST /entities/:entity_id/split`：请求 `{revision,new_entity,entry_ids,relation_ids,copy_relation_ids?,effective_at?}`，其中 new_entity 使用创建实体结构。原实体保留，指定的文献和 relation_ids 移动到新实体，copy_relation_ids 复制给新实体；同一关系不能同时迁移和复制。
- `DELETE /entities/:entity_id`：请求 `{revision}`，仅没有未排除文献关联、关系（含历史）或其他受保护引用的产品可删除；只剩排除记录时可以删除，排除记录随产品一并清理并写入操作日志。
- `GET /entities/:entity_id/reorganizations`：查看合并、拆分的来源与目标。结构变化立即生效；仅结果属性支持过去或当前生效时间。原实体的时间线及未来快照不迁移。

冲突返回 409；非法字段、跨空间引用或层级返回 400。失败不保留部分合并、拆分或转换结果。字段定义详见 [产品库演进决策](adr/0022-managed-catalog-evolution.md)。
