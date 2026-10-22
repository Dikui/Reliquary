# 外部工具接入总览

Grok Bot、外部 Agent 与采集程序负责取得原文、将 PDF 或其他格式转换为 Markdown，并提交图片。Reliquary 负责技术校验、保存、按来源标识识别重投、后台处理与检索；正常提交不逐条进行模型价值审核。综合分析由外部工具完成，再将报告写回 AI产出库。所有有效 API Key 均可访问个人空间全部库。

## 功能与接口分类

内部导航和对外能力采用相同的四类名称。HTTP 路径均以 `/api/v1` 为前缀。

| 分类 | 查询 | 操作与协议差异 |
| --- | --- | --- |
| 资料管理 | 库、资料、正文、检索、实体、主题、关注 | 沿用 `ingest`、整理、关注等 HTTP/MCP 能力 |
| 待处理 | 收录待审 | 审核、补链接及丢弃；图片缺失不产生待办 |
| 辅助诊断 | 已接收资料的处理失败、接收故障、时间线、模型调用和队列 | 读取诊断后，按当前状态使用已有审核或重新处理接口 |
| 系统配置 | 库策略、模型、系统参数、API Key | 沿用 HTTP 配置接口；本次不为全部管理操作增加 MCP 工具 |

待审使用 HTTP `GET /entries/pending` 或 MCP `list_pending`，原文使用 `GET /entries/:id/body` 或 `get_entry`。HTTP `POST /entries/:id/review` 提供 `approve`、`retry`、`supplement_url`、`admit_unverified`、`reject`、`discard`。MCP `approve_pending` 提供前三种适用审批动作 `approve`、`supplement_url`、`admit_unverified`，`discard_pending` 用于丢弃。确认与来源规则沿用现有契约。

处理失败归辅助诊断。通过 HTTP `GET /entries?parse_status=failed` 找资料，再读取 `GET /entries/:id`、`/body`、`/stages`、`/model-calls`。原文不可读时须明确报告。重试使用 `POST /entries/:id/reprocess`，批量重试使用 `POST /entries/batch-reparse`，取消使用 `POST /entries/:id/cancel-parse`。这些管理动作目前主要由 HTTP 提供。

## 连接与认证

打开网页，在设置的 API 集成为每个工具创建独立 API Key，记录后端地址。后端只监听本机，工具须运行在同一台电脑上。HTTP 使用 `Authorization: Bearer <KEY>` 与 `Content-Type: application/json`。以下示例的 BASE 是后端地址，不含 `/api/v1`。

```sh
export BASE='http://127.0.0.1:8080'
# KEY 从部署的 API 集成页取得，不写入源码或共享日志。
curl --fail "$BASE/api/v1/libraries" -H "Authorization: Bearer $KEY"
```

MCP 地址为 `$BASE/mcp`。客户端先发送 initialize，协商版本，后续按协商值发送 `MCP-Protocol-Version`，Accept 为 `application/json, text/event-stream`。同样使用 Bearer API Key。服务器支持标准 MCP 工具发现；下列示例是 tools/call 的参数，需包在 JSON-RPC 消息中。

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"Grok Bot","version":"1"}}}
```

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_libraries","arguments":{}}}
```

HTTP 响应一般使用 success/data 包装。MCP 工具结果在 content 中返回文本，支持结构化内容的工具也返回 structuredContent；工具失败检查 isError，不能只按 HTTP 200 判定成功。

## 选择库

HTTP `GET /api/v1/libraries` 对应 MCP `list_libraries`，无工具参数。每个库返回 id、directory、用途、模板、审核标准、来源证据、去重说明、accepted_fields、内容类型、关系类型和动态开关。优先使用稳定 ID 或英文 directory，不用中文显示名称作为收录或检索参数。自建库按模板执行相同规则，入库后不能换库。库 ID 不随目录更名变化。外部工具应通过 `list_libraries` 获取当前 directory，或继续使用已有库 ID。

| 库目录 | 该收什么 | 来源要求与注意事项 |
| --- | --- | --- |
| official | 机构、公司、媒体发布的新闻、公告、财报、时效研报、政策 | 外部时效型；链接必需，配置允许时可用机构加有效发布日期，标记来源未核实 |
| media | 外部人物的评论、访谈、观点文章 | 外部时效型；按库来源规则，尽量填写 author，关系证据保留发言人 |
| references | 长期引用的论文、书籍、标准、官方文档、教程 | 外部长效型；链接或 identifier，例 doi:10.1234/example |
| personal | 本人的工作资料、经验、SOP、笔记 | 自有型；可空链接，source_ref 标识同一来源，正文变化覆盖原记录 |
| ai-output | 外部 AI 写出的综合分析和报告 | 生成型；先保存、后台处理，cannot_be_fact=true |

有时效的机构研报收 official，作为定义与方法长期引用的白皮书收 references。人物评论收 media，不能因提到机构就改收 official。所有模板均接受可选 source_ref、identifier 和 graph，具体格式见 accepted_fields。

## 收录与图片提交

HTTP `POST /api/v1/ingest` 对应 MCP `ingest`，参数相同：

```json
{"items":[{"library":"personal","source_ref":"notes/calibration.md","title":"校准记录","content":"# 校准\n\n完整步骤与结果。","acquisition":"verbatim"}]}
```

每批 1 至 50 项。content 为非空可读 Markdown，允许草稿、片段和摘要，纯链接与仅图片不接收。acquisition 可选，提供时为 verbatim、extracted 或 model_written。正文及串文、引用合计最多 500 万 Unicode 码点。accepted 表示正文、图片和后台任务已保存；parse_status 查看正文索引，summary_status 查看模型处理。模型失败不撤销已建立的正文检索。不生成常规 pending_review。唯一同来源且内容未变返回 exists，有变化覆盖原 ID；缺来源标识时新建。

图片先经 `POST /api/v1/ingest/assets` 上传 multipart 的 file，再用 asset_id 和 source_url 对应正文图片地址，也可提交受限的 base64。图片可选，服务端不下载远程图片，也不生成缺图状态或待办。已有资料增补图片时，使用 `PUT /api/v1/entries/:id/body` 或 MCP `update_entry_content` 一并提交完整正文和图片。

普通 URL 查重使用 HTTP `POST /api/v1/ingest/check` 或 MCP `check_urls`，参数为 library 与 urls。完整状态、长度上限、重试及来源规则见 [收录接口](ingest-api.md)。

## 普通检索

HTTP `POST /api/v1/search` 对应 MCP `search`：

```json
{"query":"仪器校准","libraries":["personal","references"],"limit":20,"collapse_similar":false}
```

libraries 必填，可多选。支持所属库的 content_types、entity_ids、topic_ids、published_from、published_to 与 starred。结果按资料合并，返回库、摘要、命中 snippets 和正文位置。vector_unavailable 与 rerank_status 说明退回全文或原排序；检索返回 cannot_be_fact 的报告和 source_unverified 的来源时，外部工具应保留这些标记。完整坐标与过滤规则见 [检索接口](search-api.md)。MCP `get_entry` 可分段读取全文，HTTP 为 `GET /api/v1/entries/:id/body`。

## 图谱检索

HTTP `POST /api/v1/graph/search` 对应 MCP `graph_search`，参数和返回 data 字段一致：

```json
{"libraries":["official","media"],"entity":{"name":"Acme"},"mode":"neighbors","hops":2,"limit":20}
```

两点路径使用 mode=path，加 target 的 id 或 name。hops 为 1 至 3。名称不明确时 status=needs_selection，选 candidates 中的实体 ID 后重查。检索不调用模型，逐边按库过滤，从 PG 回填来源、发言人和正文位置。truncated 表示未遍历完，pending_sync 提示副本可能落后，不能据此断言没有路径。Neo4j 不可用返回明确失败。字段见 [图谱检索](search-api.md#图谱检索)，人工关系操作见 [关系管理](relations-api.md)。

## 写入 AI产出

五个库统一使用 HTTP `POST /api/v1/ingest` 或 MCP `ingest`。写入 AI产出时指定 `library: "ai-output"`，提交标题、Markdown 正文和收录方式：

```json
{"items":[{"library":"ai-output","title":"校准分析报告","content":"# 校准分析\n\n基于正文中引用资料的综合分析。","acquisition":"model_written"}]}
```

MCP 将同一对象作为 `ingest` 的 `arguments`。AI产出与其他模板共用先保存、后建正文索引并运行摘要、分类、实体和图谱的链路。相同来源且保存内容未变时返回已有资料；缺来源标识时新建。字段和状态见 [收录 API](ingest-api.md#ai产出收录)。

## 接收故障与短期故障包

接收发生技术故障时，逐项收录结果可附带 `diagnostics: {failure_id, package_status, recorded}`。原有 `status=invalid` 仍可能表示内部收录失败，应结合诊断字段判断。图片上传故障的相同引用位于错误的 `details.diagnostics`。`recorded=false` 表示故障包未成功落盘；若运行事件已保存，仍可按故障编号查询 `unavailable` 摘要，但不能读取原始材料。事件也写入失败时不承诺能查到。参数错误、格式错误、超限和认证失败不保留提交内容或故障包。

| 能力 | HTTP | MCP |
| --- | --- | --- |
| 故障列表 | `GET /diagnostics/ingest-failures` | `list_ingest_failures` |
| 故障详情与材料清单 | `GET /diagnostics/ingest-failures/:id` | `get_ingest_failure` |
| 读取材料 | `GET /diagnostics/ingest-failures/:id/materials/:material_id` | `read_ingest_failure_material` |

列表参数：`page` 默认 1，`page_size` 默认 20、最大 100；`from`、`to` 使用 RFC3339 时间；可按 `api_key_id`、`library_id`、`error_code`、`request_id`、`id` 筛选。不限定库时也能找到尚未解析出目标库的故障。HTTP 返回 `success/data`，列表的 `data` 为 `{items,total,page,page_size}`。MCP 按已有 `structuredContent` 和 `isError` 规则返回。

详情包括执行阶段、错误、结果判断、关联请求/资料、非敏感执行条件、材料清单和过期时间。包状态为 `available`（输入与已知条件可用）、`partial`（查看 `missing`）、`unavailable`、`expired`。`available` 不承诺外部服务和模型输出可逐字复现。`outcome=rolled_back` 表示已知回滚；`unknown` 表示应先核对资料与任务，不能盲目重试。

材料标识只能取自清单。文本的 `offset`、`length` 按 Unicode 码点计数；默认读取 20000、最多 100000，使用返回的 `next_offset` 继续读取。HTTP 图片返回原始图片字节；MCP 图片返回 `url` 和元数据，须继续携带 Bearer 凭据访问该相对 HTTP 地址。不存在或无权读取返回 404；已过期材料返回 410。读取材料不会执行重放。

```json
{"name":"list_ingest_failures","arguments":{"request_id":"原请求编号","page_size":20}}
```

```json
{"name":"read_ingest_failure_material","arguments":{"id":"故障编号","material_id":"清单中的材料编号","offset":0,"length":20000}}
```

修复时先读取原始输入、实际图片及执行条件，再检查是否已有资料或任务。修正后通过普通 `ingest` 重提，可在条目中携带 `recovery_failure_id` 关联原故障。该字段不影响正文、来源身份和去重。原快照保持不变，详情记录新的资料和请求编号。重提仍失败时，新故障的 `recovery_failure_id` 指向上一次故障。仍须继续确认结果；`accepted` 只表示已接收，待审或处理失败须如实报告。确认拒绝或删除前，清理关联恢复链及进行中的故障材料。清理失败会保留资料并返回错误，修复存储后可重试。

故障包保留 7 天，总容量 512 MiB，先过期再淘汰最旧材料。详见[日志与故障包配置](logging.md#接收故障日志与故障包)。

## 资料 ID

HTTP、MCP 和文件库统一使用 Entry 命名。资料 ID 为 `entry_id`，列表为 `entry_ids`，更新指针为 `latest_entry_id`，关联资料为 `related_entry_id` 或 `related_entry_ids`。MCP 读取工具为 `get_entry`，实体详情返回 `entries`。不接受旧字段或旧工具名称的别名。资料对象与文件库表头自身的主键保持 `id`，读取 HTTP 路径保持 `/api/v1/entries/:id`。

测评集与运行、检索记录、模型调用统计通过 HTTP 管理，分别见 [测评 API](search-evaluation-api.md) 与 [调用统计](model-calls.md)。当前没有这些管理操作的 MCP 工具。备份、恢复、队列重推及图谱重建由部署维护者执行，见 [个人使用](personal-use.md)。
