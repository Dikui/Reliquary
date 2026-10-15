# 关系正本 HTTP 接口

资料标识统一为 `entry_id`，资料列表使用 `entry_ids`，不接受旧字段别名。
接口位于 `/api/v1`，网页不带凭据，外部工具使用 API Key 认证，范围为个人空间。图谱检索的 HTTP 与 MCP 用法见 [图谱检索](search-api.md#图谱检索)。关系管理操作使用本文 HTTP 接口。

- `GET /entities/:entity_id/relations?library_id=<库 ID>`：可选库过滤，未传时返回全部库；按 `relation_type` 分组。每条含关系 ID、全局主体与客体 ID、库 ID 与名称、版本、方向 `incoming/outgoing`、对端 `peer`、`source_count` 和 `sources`。每个来源含资料 ID、标题、运行 ID、正文哈希与 `evidence`；每份证据含 `locations` 和可选 `speaker`，结构沿用处理输出。已合并实体 ID 自动定位到目标实体。成功返回 `{"success":true,"data":[]}`，无关系时为空数组。
- `DELETE /relations/:relation_id`：删除关系和所有来源，保存所属库的三元组抑制与 `delete_relation` 日志。同一事务写入两端实体的待同步记录。不存在或不属于空间时返回 404；成功返回 `{"success":true}`。
- `POST /libraries/:id/relation-types/merge`：请求 `{"from_type":"合作","to_type":"投资"}`。两种类型须在库内，合并现有关系与来源、改写抑制、禁用旧类型并保存持久映射及 `merge_relation_types` 日志。之后抽出的旧名称仍沿映射链定位到最终类型。重复提交已完成的同一合并幂等；已合并的来源类型不能另改目标。成功返回 `{"success":true}`。

位置以保存后的 Markdown 正文 Unicode 字符范围为坐标，包含原有标题路径、段落序号与正文哈希。一个来源可以有多个发言人与位置，来源数量仍按资料计数。按实体列关系使用同一 PostgreSQL 只读快照读取版本、来源及数量。

## 人工动作与副本一致性

修改内容类型使用 `PUT /api/v1/entries/:id/content-type`，请求 `{"content_type":"所属库启用类型"}`。按资料 graph 覆盖和类型默认值决定补抽或撤除关系。实体合并使用 `POST /api/v1/entities/merge`，请求 `{"keep_entity_id":"保留ID","merge_entity_id":"被合并ID"}`。删除资料使用 `DELETE /api/v1/entries/:id`，同时撤除该资料的关系来源，最后一个来源删除后关系也删除。按资料 ID 补图后重新处理会替换该资料的证据，不累加重复来源。

这些动作与 PG 正本及待同步通知在同一事务提交。Neo4j 异步读取 PG 当前状态；查询返回的 `pending_sync` 可提示副本落后。人工删除留下库内抑制，重新处理不能复活该三元组。自有型同一 `source_ref` 仅最新活动资料参与图谱，旧资料仍可普通检索。维护核对见 [个人使用指南](personal-use.md#neo4j-图谱查询副本)。
