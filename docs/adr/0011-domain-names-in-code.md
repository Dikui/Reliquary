# 0011 代码中的领域命名

Status: accepted
日期：2026-10-12

## 背景

Reliquary 是 Tencent WeKnora 的分支，代码沿用了上游的多租户、多知识库命名。经过 [0007](0007-personal-single-user.md) 与 [0009](0009-kb-collect-store-retrieve.md) 的收窄，`CONTEXT.md` 中的概念已经和代码名字对不上：

- 个人空间在代码中叫 `Tenant`（表 `tenants`，几乎每张表都有 `tenant_id`），另有 `single_team` 文件沿用已废弃的“团队空间”。
- 知识库中的一行内容（文章、用户文档、整理结果，以 `source_kind` 区分）在代码中叫 `Knowledge`（表 `knowledges`），较新的代码又叫 `article`（`article_embeddings`、`article_search`），同一概念有两个名字，而“文章”在术语表中只指外部收录的内容。
- 检索相关代码分散在 `knowledge_search/`、`knowledge_search_service.go`、`knowledgebase_search*.go`、`article_search*.go` 四组文件中。

## 决策

- **个人空间**在代码中叫 `Space`：表 `spaces`，外键列 `space_id`，API Key 表 `api_keys`、类型 `APIKey`。`single_team` 相关文件并入 space 命名。HTTP 路径已经是 `/space`，保持不变。
- 新增术语**条目**（Entry）：知识库中的一行内容，`source_kind` 为 `article`（文章）、`mine`（用户文档）或 `report`（整理结果）。代码中叫 `Entry`：表 `entries`，外键列 `entry_id`，关联表使用 `entry_` 前缀。
- `Article` 只用于专属文章（`source_kind = article`）的逻辑，例如收录、去重与转载归并。
- **知识库**仍叫 `KnowledgeBase`，`knowledge_base_id` 列保留。单独的 `knowledge` 或 `Knowledge` 不再作为标识符使用：指整个知识库时用 `KnowledgeBase` 或 `kb`，指一行内容时用 `Entry`，对整个知识库的功能（检索、审核、主题）直接用功能名。
- 数据库通过新的版本化迁移改名，不修改已冻结的 `000000_init` 基线。维护副本（`.reliquary/`）提升格式版本，并能读取旧版本副本。
- 已持久化的取值不变，包括任务类型字符串、操作日志类型、关注对象类型、`source_kind` 的取值、设置键、jsonb 内部的键与文件库路径。如需改动，由迁移一并转换。
- 对外契约不变：MCP 工具名与字段、`POST /api/v1/ingest` 的请求与响应、文件库表头字段。网页前端使用的 HTTP 路径与 JSON 字段随代码一起改名，不保留旧别名。
- 界面中文文案不变。

## 后果

改名分两步完成（个人空间、条目），每步包含数据库迁移、维护副本格式与前后端代码。旧数据库通过迁移升级，旧备份仍可恢复。

调用前端 HTTP 接口（`/api/v1/ingest` 以外）的外部脚本需要按新路径和字段调整。MCP 中的 `get_article` 等工具名继续用“文章”泛指条目，这是为稳定对外契约保留的不一致。

`space_id` 列在单用户部署中恒为 1，是否删除这一列不在本决策范围内。
