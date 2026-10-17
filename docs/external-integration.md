# 外部工具接入总览

Grok Bot、外部 Agent 与采集程序负责取得原文、将 PDF 或其他格式转换为 Markdown，并提交图片。Reliquary 负责按库审核、保存、去重、处理与检索。综合分析由外部工具完成，再将报告写回 AI 报告库。所有有效 API Key 均可访问个人空间全部库。

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

HTTP `GET /api/v1/libraries` 对应 MCP `list_libraries`，无工具参数。每个库返回 id、directory、用途、模板、审核标准、来源证据、去重说明、accepted_fields、内容类型、关系类型和动态开关。优先使用稳定 ID 或英文 directory，不用中文显示名称作为收录或检索参数。自建库按模板执行相同规则，入库后不能换库。

| 库目录 | 该收什么 | 来源要求与注意事项 |
| --- | --- | --- |
| institutional | 机构、公司、媒体发布的新闻、公告、财报、时效研报、政策 | 外部时效型；链接必需，配置允许时可用机构加有效发布日期，标记来源未核实 |
| opinions | 外部人物的评论、访谈、观点文章 | 外部时效型；按库来源规则，尽量填写 author，关系证据保留发言人 |
| references | 长期引用的论文、书籍、标准、官方文档、教程 | 外部长效型；链接或 identifier，例 doi:10.1234/example |
| mine | 本人的工作资料、经验、SOP、笔记 | 自有型；可空链接，source_ref 标识同一来源，正文变化追加新记录 |
| ai-reports | 外部 AI 写出的综合分析和报告 | 生成型；免审核与内容处理，只建索引，cannot_be_fact=true |

有时效的机构研报收 institutional，作为定义与方法长期引用的白皮书收 references。人物评论收 opinions，不能因提到机构就改收 institutional。各库不可接受的字段以 accepted_fields 为准，不能给非自有型提交 source_ref。

## 收录与补图

HTTP `POST /api/v1/ingest` 对应 MCP `ingest`，参数相同：

```json
{"items":[{"library":"mine","source_ref":"notes/calibration.md","title":"校准记录","content":"# 校准\n\n完整步骤与结果。","acquisition":"verbatim"}]}
```

每批 1 至 50 项。content 是完整 Markdown；acquisition 必选 verbatim、extracted 或 model_written。新收录最多 500 万 Unicode 码点，实际处理还受模型上下文及分段数限制。逐项检查 status、entry_id、reason、image_failures 和 completeness。accepted 表示接收成功，处理可异步完成；用 `GET /api/v1/entries/:id` 查询 parse_status。pending_review 需要用户判断，invalid 需要修正参数，exists 使用已有 ID。

图片先经 `POST /api/v1/ingest/assets` 上传 multipart 的 file，再用 asset_id 和 source_url 对应正文图片地址，也可提交受限的 base64。服务端不下载远程图片。暂缺图片时提交 missing，后续按资料 ID 补全：

```json
{"items":[{"library":"mine","entry_id":"缺少图片的已有资料ID","images":[{"asset_id":"上传返回ID","source_url":"https://example.org/chart.png"}]}]}
```

普通 URL 查重使用 HTTP `POST /api/v1/ingest/check` 或 MCP `check_urls`，参数为 library 与 urls；不是图片补全入口。完整状态、长度上限、重试及来源规则见 [收录接口](ingest-api.md)。

## 普通检索

HTTP `POST /api/v1/search` 对应 MCP `search`：

```json
{"query":"仪器校准","libraries":["mine","references"],"limit":20,"collapse_similar":false}
```

libraries 必填，可多选。支持所属库的 content_types、entity_ids、topic_ids、published_from、published_to 与 starred。结果按资料合并，返回库、摘要、命中 snippets 和正文位置。vector_unavailable 与 rerank_status 说明退回全文或原排序；检索返回 cannot_be_fact 的报告和 source_unverified 的来源时，外部工具应保留这些标记。完整坐标与过滤规则见 [检索接口](search-api.md)。MCP `get_entry` 可分段读取全文，HTTP 为 `GET /api/v1/entries/:id/body`。

## 图谱检索

HTTP `POST /api/v1/graph/search` 对应 MCP `graph_search`，参数和返回 data 字段一致：

```json
{"libraries":["institutional","opinions"],"entity":{"name":"Acme"},"mode":"neighbors","hops":2,"limit":20}
```

两点路径使用 mode=path，加 target 的 id 或 name。hops 为 1 至 3。名称不明确时 status=needs_selection，选 candidates 中的实体 ID 后重查。检索不调用模型，逐边按库过滤，从 PG 回填来源、发言人和正文位置。truncated 表示未遍历完，pending_sync 提示副本可能落后，不能据此断言没有路径。Neo4j 不可用返回明确失败。字段见 [图谱检索](search-api.md#图谱检索)，人工关系操作见 [关系管理](relations-api.md)。

## 写入 AI 报告

HTTP `POST /api/v1/reports` 对应 MCP `write_report`，固定写入 AI 报告系统库：

```json
{"title":"校准分析报告","markdown":"# 校准分析\n\n基于引用资料的综合分析。","related_entry_ids":["已有资料ID"],"related_entities":["已有实体ID"],"request_id":"grok-calibration-20261014-01"}
```

title 与 markdown 必填。related_entities 只引用已存在且未合并的实体，topics 可引用已有主题；不自动创建档案。不传来源、摘要或图谱开关。相同 request_id 的重试返回原报告，existing=true，不覆盖内容。没有 request_id 时按规范内容指纹幂等。报告只建索引，向量调用仍记账；index_status=pending_retry 时由后台继续，不必再创建另一份。完整格式与幂等边界见 [报告契约](ingest-api.md#write_reportai-报告格式契约)。

## 资料 ID

HTTP、MCP 和文件库统一使用 Entry 命名。资料 ID 为 `entry_id`，列表为 `entry_ids`，更新指针为 `latest_entry_id`，关联资料为 `related_entry_id` 或 `related_entry_ids`。MCP 读取工具为 `get_entry`，实体详情返回 `entries`。不接受旧字段或旧工具名称的别名。资料对象与文件库表头自身的主键保持 `id`，读取 HTTP 路径保持 `/api/v1/entries/:id`。

测评集与运行、检索记录、模型调用统计通过 HTTP 管理，分别见 [测评 API](search-evaluation-api.md) 与 [调用统计](model-calls.md)。当前没有这些管理操作的 MCP 工具。备份、恢复、队列重推及图谱重建由部署维护者执行，见 [个人使用](personal-use.md)。
