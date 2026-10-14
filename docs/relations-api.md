# 关系正本 HTTP 接口

接口位于 `/api/v1`，使用现有登录或 API Key 认证，范围为个人空间。图谱检索及 MCP 接口由 ikb-14 提供。

- `GET /entities/:entity_id/relations?library_id=<库 ID>`：可选库过滤，未传时返回全部库；按 `relation_type` 分组。每条含关系 ID、全局主体与客体 ID、库 ID 与名称、版本、方向 `incoming/outgoing`、对端 `peer`、`source_count` 和 `sources`。每个来源含资料 ID、标题、运行 ID、正文哈希与 `evidence`；每份证据含 `locations` 和可选 `speaker`，结构沿用处理输出。已合并实体 ID 自动定位到目标实体。成功返回 `{"success":true,"data":[]}`，无关系时为空数组。
- `DELETE /relations/:relation_id`：删除关系和所有来源，保存所属库的三元组抑制与 `delete_relation` 日志。同一事务写入两端实体的待同步记录。不存在或不属于空间时返回 404；成功返回 `{"success":true}`。
- `POST /libraries/:id/relation-types/merge`：请求 `{"from_type":"合作","to_type":"投资"}`。两种类型须在库内，合并现有关系与来源、改写抑制、禁用旧类型并保存持久映射及 `merge_relation_types` 日志。之后抽出的旧名称仍沿映射链定位到最终类型。重复提交已完成的同一合并幂等；已合并的来源类型不能另改目标。成功返回 `{"success":true}`。

位置以保存后的 Markdown 正文 Unicode 字符范围为坐标，包含原有标题路径、段落序号与正文哈希。一个来源可以有多个发言人与位置，来源数量仍按资料计数。按实体列关系使用同一 PostgreSQL 只读快照读取版本、来源及数量。
