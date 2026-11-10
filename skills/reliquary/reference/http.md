# HTTP 参考

没有 MCP 时，先读 [使用规则](../rules.md)，再按 [流程索引](../SKILL.md#流程索引) 读取对应流程，用下表的 HTTP 接口完成同样的步骤。只连接本机后端，默认地址 `http://127.0.0.1:8080`。下表路径均带 `/api/v1` 前缀；`:id` 等占位符须替换成目标 ID。

## 认证与响应

每次请求发送 `Authorization: Bearer <RELIQUARY_API_KEY>`，Key 从环境变量读取，不写进技能或配置。JSON 请求发送 `Content-Type: application/json`。GET 参数放查询串，正文接口与路径参数的例外见表。

大多数 JSON 成功响应为 `{"success":true,"data":...}`，业务结果在 `data`。先检查 HTTP 状态码和 `success`；失败可能含 `error` 或 `message`，不能只看有无 `data`。列表的包装差异、无 `data` 的成功响应和原始正文响应见下表。收录批次即使 HTTP 成功，也要逐项检查 `items` 的 `status`、`reason`、`entry_id`，保存成功不代表后台处理完成。

MCP 端点为 `/mcp`，使用同一 Bearer 头。MCP 调用还须检查 JSON-RPC 错误及工具结果的 `isError`，HTTP 200 或客户端未报传输错误不代表工具成功。普通工具返回 `structuredContent` 及 JSON 文本内容，不带 HTTP 的 `success` 包装；`reliquary_guide` 返回流程文本。MCP 通用投影将 `space_id` 映射成 `tenant_id`，不应据此向 HTTP 传空间参数。

## 工具与接口

| MCP 工具 | HTTP 方法与路径 | 参数与结果差异 |
|---|---|---|
| `list_libraries` | GET `/api/v1/libraries` | 无参数；HTTP `data` 是库数组，MCP 为 `libraries` 数组 |
| `search` | POST `/api/v1/search` | JSON 参数同 MCP，必填 `query`、`libraries`；`data` 含 `results`、降级状态和缺发布时间排除数 |
| `shelf_find` | GET `/api/v1/libraries/:id/shelf/find` | MCP 的 library_id 放路径；q、limit、offset 放查询串；返回 items、total、truncated，候选含类别 |
| `shelf_get` | GET `/api/v1/libraries/:id/shelf/nodes/:node_id` | library_id、node_id 放路径；as_of 为 RFC3339，combination 为 URL 编码的部件类型到部件 JSON 对象；返回 entity、shelf、facts、as_of、recorded |
| `shelf_query` | POST `/api/v1/libraries/:id/shelf/query` | library_id 放路径，其余 JSON 筛选同 MCP；返回 items、total、truncated、as_of |
| `shelf_guide` | 仅 MCP | 必传 library_id，可选 node_id；返回本库补充规范、可用值、已保存方案与可选缺口；原件另调 POST `/api/v1/libraries/:id/entity-attachments` 暂存 |
| `shelf_submit` | POST `/api/v1/libraries/:id/shelf/changesets`；PATCH `/api/v1/libraries/:id/shelf/changesets/:changeset_id`；POST `/api/v1/libraries/:id/shelf/changesets/:changeset_id/confirm` | 新建时 library_id 放路径，其余同 MCP；继续处理时 HTTP 保存决定用 PATCH `/api/v1/libraries/:id/shelf/changesets/:changeset_id`，确认用 POST confirm 路径，JSON 为 revision、decisions。MCP 用同一工具的 changeset_id、revision、decisions，且确认必须 confirmed=true；错误含 error.details 中的变更集 |
| `get_entry` | GET `/api/v1/entries/:id`，GET `/api/v1/entries/:id/body` | 前者 `data` 为资料详情；后者为原始 Markdown。与 MCP 分段读取不同，详见下文 |
| `graph_search` | POST `/api/v1/graph/search` | JSON 参数同 MCP，必填 `libraries`、`entity`；`data` 含候选、边、证据、截断及未同步状态 |
| `list_topics` | GET `/api/v1/topics` | 查询参数 `library_id`；HTTP `data` 是数组，MCP 为 `topics` 数组 |
| `create_topic` | POST `/api/v1/topics` | JSON `library_id`、`name`、可选 `description`；201，`data` 为主题 |
| `update_topic` | PUT `/api/v1/topics/:id` | ID 放路径，JSON `name`、`description` 按需传；`data` 为主题 |
| `list_entities` | GET `/api/v1/entities` | 查询参数 `library_id`、可选 `entity`、`entity_type`、`limit`、`offset`；HTTP `data` 是数组，MCP 为 `entities` 数组 |
| `get_entity` | GET `/api/v1/entities/:entity_id` | ID 放路径；`data` 为实体详情与资料列表，已合并 ID 解析到保留档案 |
| `list_follows` | GET `/api/v1/follows` | 无参数；HTTP `data` 是数组，MCP 为 `follows` 数组 |
| `follow_feed` | POST `/api/v1/follows/feed` | JSON 参数同 MCP；`data` 含 `results`、各库状态和 `next_cursor`，不更新查看时间 |
| `follow` | PUT `/api/v1/follows/:type/:id` | `target_type`、`target_id` 分别放路径；无需 JSON 正文，`data` 为关注详情 |
| `unfollow` | DELETE `/api/v1/follows/:type/:id` | 同上；HTTP 仅返回 `success=true`，MCP 返回请求目标和 `following=false` |
| `ingest` | POST `/api/v1/ingest` | JSON `items` 数组同 MCP，逐项 `library` 必填，正文为 Markdown；`data` 含逐项收录结果 |
| `update_entry_content` | PUT `/api/v1/entries/:id/body` | `entry_id` 放路径，JSON `content`、可选 `acquisition`、`images`；`data` 为更新结果 |
| `delete_document` | DELETE `/api/v1/documents/:id` | `document_id` 放路径，仅支持旧版导入的个人资料（`source_kind=mine`）；不传 `confirmed`，`data` 含删除状态 |
| `list_pending` | GET `/api/v1/entries/pending` | 查询参数 `kind`、`page`、`page_size`；HTTP `data` 是数组，`total`、`page`、`page_size` 在响应顶层；MCP 将这四个字段一起返回 |
| `approve_pending` | POST `/api/v1/entries/:id/review` | JSON 显式传 `action`；批准为 `approve`；补链接为 `supplement_url`，链接放 `source_info.original_url`，不同于 MCP 顶层 `url`；无链接入库为 `admit_unverified`，须 `confirmed=true`；`data` 为资料 |
| `discard_pending` | POST `/api/v1/entries/:id/review` | JSON `{"action":"discard"}`；不需要 `confirmed`，`data` 为被丢弃资料 |
| `set_star` | PUT `/api/v1/entries/:id/star` | ID 放路径，JSON `starred` 必填；`data` 为更新后的资料 |
| `check_urls` | POST `/api/v1/ingest/check` | JSON `library`、`urls` 同 MCP；`data.items` 含逐条状态与资料 ID |
| `list_ingest_failures` | GET `/api/v1/diagnostics/ingest-failures` | 查询参数同 MCP：`page`、`page_size`、`from`、`to`、`api_key_id`、`library_id`、`error_code`、`request_id`、`id`；`data` 为故障分页结果 |
| `get_ingest_failure` | GET `/api/v1/diagnostics/ingest-failures/:id` | `data` 为故障详情与材料清单，不执行重放 |
| `read_ingest_failure_material` | GET `/api/v1/diagnostics/ingest-failures/:id/materials/:material_id` | 文本支持查询 `offset`、`length`，`data` 为 Unicode 分段结果；图片返回原始图片字节，MCP 返回需 Bearer 认证的读取地址 |
| `list_content_checks` | GET `/api/v1/diagnostics/content-checks` | 查询 `page`，默认 1，每页固定 20；`data` 含 `items`、`total`，列出运行 |
| `get_content_check` | GET `/api/v1/diagnostics/content-checks/:id` | ID 为运行 ID；`data` 含运行状态与 `results`，其中 `findings` 含问题及原文证据，不是单条问题接口 |
| `reliquary_guide` | 无 HTTP 路由 | 直接读取 [SKILL.md 流程索引](../SKILL.md#流程索引) 中的 7 个文件；不要请求不存在的指南接口 |

## 正文与写操作

HTTP 资料详情不等于 MCP `get_entry` 的表头与分段正文投影。用详情核对元数据及标记，再 GET `/api/v1/entries/:id/body` 取完整已保存 Markdown，按 [位置字段](fields.md#位置与引用) 在本地按 Unicode 码点切片。正文响应的 `ETag` 是带引号的 SHA-256；可与证据的 `body_hash` 核对。HTTP 正文支持字节 `Range`，但不支持 MCP 的 Unicode `offset`、`length`、`next_offset` 查询，不能把 Unicode 位置直接用作字节范围。

所有写操作仍遵守使用规则：先向用户展示目标、内容和影响并取得明确同意。HTTP 删除和丢弃不需要 MCP 的 `confirmed` 参数，也不会返回 MCP 的 `confirmation_required` 提示，agent 仍须先问用户，不能切换 HTTP 来跳过确认。删除或丢弃会清理相关资料与来源并记录操作日志，不能恢复；正文覆盖不保留旧版本。HTTP 的 `admit_unverified` 仍需它自身的 `confirmed=true`，不要与 MCP 删除确认混淆。

自检的两个诊断 GET 接口只读已有运行和证据。自检设置写入与复检只允许本机网页会话，不提供 MCP 工具；HTTP agent 不应调用它们绕过复核流程的只读范围。图片若需先上传，可用 POST `/api/v1/ingest/assets` 发送 multipart `file`，取 `data.asset_id` 再写入，上传也须在用户授权范围内。

货架范围检索可给 search 传 shelf_node_ids，范围含节点、下级、组成部件及其认证附件，只接受已确认的资料文件归属。默认排除旧版本；include_history=true 时同一文件合并返回，保留各版本独立的文本引用，注明历史命中。没有明确归属的独立文本仍可普通检索。

货架写入沿用货架专家流程。提交操作列表且省略 confirmed 只保存待确认变更集，不写主数据。用户明确同意差异后才确认；含义不确定不能用普通接受跳过。HTTP 继续处理时先 GET `/api/v1/libraries/:id/shelf/changesets/:changeset_id` 读取当前修订和差异，确认失败检查 error.details，不能绕过确认或并发检查。
