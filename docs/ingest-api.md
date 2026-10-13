# 收录 API

Reliquary 接受外部工具转换好的 Markdown 正文和图片，不抓取链接，不保存 PDF、Office 等原件。HTTP 与 MCP 共用收录服务。认证为 `Authorization: Bearer <API Key>`。先调用 MCP `list_libraries` 或 HTTP `GET /api/v1/libraries`，选择库的 `id` 或英文 `directory`；响应的 `accepted_fields` 说明该库接受的字段。

## 请求字段

`POST /api/v1/ingest` 接收 `{"items":[...]}`。单条请求使用一个元素，批量为 1 至 50 个元素。每项独立校验，缺库、库不存在、库不接受某字段时，该项返回 `status=invalid`，不影响其他有效项。

| 字段 | 规则 |
| --- | --- |
| `library` | 每项必填，库 ID 或英文目录名。不能用中文名称。缺少或不存在时提示调用 `list_libraries` |
| `content` | 新收录必填，非空 Markdown 全文，不能只有一个链接。上限 5,000,000 Unicode 码点；补全时可省略 |
| `acquisition` | 新收录必填：`verbatim` 逐字原文、`extracted` 提取、`model_written` 模型整理 |
| `source_ref` | 仅自有型库接受。可选字符串，去首尾空白，最多 2048 Unicode 码点。同库同来源按最新记录的正文判断重复 |
| `identifier` | 仅外部长效型库接受。可选 `scheme:value`，最多 2048 Unicode 码点。去两部分首尾空白，scheme 小写，DOI 的 value 也小写。其他 scheme 的值保留大小写。本阶段保存和返回，尚不参与去重 |
| `graph` | 可选布尔值，保存在资料上；未提供或 null 表示按库和内容类型默认。本阶段不执行图谱抽取 |
| `article_id` | 仅补全使用，已有资料 ID。Go 字段名为 `EntryID`，JSON 沿用收录结果及 MCP 的 `article_id`，避免同时出现两套 ID 名称 |
| `url` | 可选来源链接，可为空；非空必须为 HTTP 或 HTTPS，最多 2048 字节。现有外部来源审核仍可能要求链接 |
| `title` | 可选标题，最多 255 个码点；AI 报告需要标题 |
| `author`、`publisher`、`published_at` | 可选来源信息；日期使用 YYYY-MM-DD 或 RFC3339 |
| `images` | 可选图片列表，最多 100 项，见下节 |
| `missing` | 仅图片缺失：`type=image`、`source_url` 和非空 `reason` |
| `content_type_hint` | 可选分类提示，不替代处理结果 |
| `thread`、`quotes` | 可选有序正文片段与引用；每项 `content` 必填，合计正文仍受 500 万码点限制 |
| `collect_reason` | 收藏理由 |
| `agent_summary`、`agent_tags`、`source_name` | 调用方注释，保存数据库，不进入正文或处理提示词 |

`kind`、`document_id`、`file` 和未知字段被拒绝。向其他模板提交空字符串 `source_ref` 或 `identifier` 也会被拒绝。

| 系统库目录 | 模板 | 专有可选字段 |
| --- | --- | --- |
| `institutional`，机构发布 | `external_timely` | 无 |
| `opinions`，人物观点 | `external_timely` | 无 |
| `references`，参考文献 | `external_durable` | `identifier` |
| `mine`，我的资料 | `own` | `source_ref` |
| `ai-reports`，AI 报告 | `generated` | 无 |

自建库按模板接受相同字段。`graph` 五个库都可保存。除生成型外，沿用现有审核和处理流程；本阶段未实施新的库级审核、来源证据与转载归并规则。生成型收录调用报告保存与索引流程，不审核、不调用处理模型。推荐外部 Agent 通过 `write_report` 写报告。

## 五个系统库示例

以下请求均发送到 `/api/v1/ingest`。例如：

```sh
curl -X POST "$BASE/api/v1/ingest" \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  --data-binary '{"items":[{"library":"institutional","url":"https://example.org/release","title":"机构公告","publisher":"示例研究院","content":"# 公告\n\n完整公告正文与测量方法。","acquisition":"extracted","graph":true}]}'
```

人物观点：

```json
{"items":[{"library":"opinions","url":"https://example.org/interview","title":"访谈观点","author":"张三","publisher":"示例访谈","content":"# 访谈\n\n完整访谈记录。","acquisition":"verbatim"}]}
```

参考文献：

```json
{"items":[{"library":"references","url":"https://example.org/paper","title":"实验论文","publisher":"示例期刊","identifier":"DOI:10.1234/EXAMPLE","content":"# 论文\n\n完整研究方法与结果。","acquisition":"extracted"}]}
```

我的资料：

```json
{"items":[{"library":"mine","source_ref":"notes/calibration.md","title":"校准记录","content":"# 校准\n\n仪器读数为 42。","acquisition":"verbatim","graph":false}]}
```

AI 报告：

```json
{"items":[{"library":"ai-reports","title":"研究报告","content":"# 研究报告\n\n综合分析内容。","acquisition":"model_written","graph":false}]}
```

## 来源标识与结果

自有型库同一 `source_ref` 的正文，按规范 Markdown（含追加片段、引用和图片稳定标识）去首尾空白、统一空白后计算 SHA-256，与最新一条比较。相同则返回 `exists` 和已有 `article_id`；不同则保存新资料，保留旧记录。再次提交较早的正文也会产生新记录。无 `source_ref` 每次独立收录；不同库互不影响。

资料详情、HTTP 收录结果、MCP 收录与 `get_article` 返回 `source_ref`、`identifier`、`graph`、`has_update`、`latest_article_id`。`has_update=true` 表示存在更新记录，`latest_article_id` 指向同库同来源最新的未删除记录。最新记录本身返回 false 与自己的 ID；无来源标识时不提供最新 ID。历史记录的查询、正文与时间保持不变。

逐项结果另含 `index`、`library_id`、`article_id`、`status`、`message`、`reason`、`image_failures`、`completeness`。状态包括 `accepted`、`exists`、`supplemented`、`pending_review`、`conflict`、`discarded`、`invalid`。无效项原因可包含：

| 错误 | 返回方式与信息 |
| --- | --- |
| 缺库或库不存在 | HTTP 200 的对应项 `invalid`，原因包含 `library` 与 `call list_libraries` |
| 字段不适用于库 | 对应项 `invalid`，原因包含 `source_ref is only accepted by own libraries` 或 `identifier is only accepted by external_durable libraries` |
| 错误、跨库或非待补全 ID | 对应项 `invalid`，原因分别包含 `does not identify an existing entry`、`different library`、`not awaiting image completion` |
| 正文缺失或超长、标识格式无效 | 对应项 `invalid`，或流式读取时立即拒绝超长请求；给出具体字段与上限 |
| JSON 语法、批量包络或字节上限错误 | HTTP 400，应用错误码 `1000`；MCP 返回工具错误，流式读取超限为带具体原因的 HTTP 400 |

`POST /api/v1/ingest/check` 接收 `{"library":"institutional","urls":["https://example.org/release"]}`。每次 1 至 100 个链接；缺库或库不存在时每个链接返回 `invalid` 和原因。已存在结果为 `in_library` 或 `pending_review`；其他状态为 `not_found`、`discarded`。自有型资料不按链接判重。外部来源完整去重规则的库内化由 ikb-06 完成。

## 图片与按资料 ID 补全

先上传图片：

```sh
curl -X POST "$BASE/api/v1/ingest/assets" \
  -H "Authorization: Bearer $KEY" -F 'file=@chart.png'
```

上传只接受真实图片，支持 png、jpg、jpeg、gif、bmp、tiff、webp，不接受 SVG。用返回的 `asset_id`，或在 `data` 中提供 base64/data URI；两者不能同时提供。`source_url` 对应正文中的原图片地址。

首次收录缺图片：

```json
{"items":[{"library":"mine","source_ref":"notes/figure.md","title":"测量记录","content":"# 测量\n\n完整正文。\n\n![图表](https://example.org/chart.png)","acquisition":"verbatim","missing":[{"type":"image","source_url":"https://example.org/chart.png","reason":"暂未取得图片"}]}]}
```

补全只凭库与资料 ID 定位，无须 URL 或再次提交正文：

```json
{"items":[{"library":"mine","article_id":"首次收录返回的 article_id","images":[{"asset_id":"上传返回的 asset_id","source_url":"https://example.org/chart.png"}]}]}
```

补全把正文中匹配的图片引用替换为本地路径，文字保持原样。可选 `content` 用于旧有正文冲突检查；不同正文另存关联收录待审，返回 `conflict`，不覆盖原资料。保留原有规范化与近似正文比较规则。不能用相同 URL 自动补全。图片替换位置预留 ikb-08 重新计算正文坐标的调用点。

`GET /api/v1/ingest/incomplete?page=1&page_size=20` 列出待补全资料，含 `article_id`、`library_id` 与缺失图片。`include_attempted=false` 排除已尝试补全一次的资料。`POST /api/v1/ingest/:id/accept-incomplete` 接受缺失，`POST /api/v1/ingest/:id/discard` 丢弃资料，可附 `reason`。

## 长度和传输上限

正文安全上限为 5,000,000 Unicode 码点。中文通常占 15,000,000 UTF-8 字节，全部四字节字符为 20,000,000 字节。单项 JSON 上限 64 MiB，即 67,108,864 字节，容纳正文与图片传输开销。批量 JSON 总上限 128 MiB，即 134,217,728 字节；MCP HTTP 消息含协议包络上限 129 MiB，即 135,266,304 字节。即使每项均未超限，批量也可能超限，应拆批。

内联图片每张解码后最多 10 MiB，总计最多 20 MiB；20 MiB 的 base64 约 26.7 MiB。图片上传还受部署的上传限制。流式读取器在读到正文码点或总字节上限时停止，报错而不截断，不读取剩余无限数据。不要依赖 Content-Length 检查代替流式限制。

## MCP

`ingest` 使用同样的 `items` 包络、字段与逐项状态。例如：

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ingest","arguments":{"items":[{"library":"mine","source_ref":"notes/mcp.md","content":"# 笔记\n\n通过 MCP 收录的正文。","acquisition":"verbatim","graph":false}]}}}
```

先 `initialize`，随后请求 `/mcp`，使用 Bearer API Key、`Accept: application/json, text/event-stream` 和协商的 `MCP-Protocol-Version`。`list_libraries` 返回库用途、模板、审核和来源说明、去重说明及 `accepted_fields`。`check_urls` 参数与 HTTP 相同。`list_incomplete`、`accept_incomplete` 可用于发现和处理缺图片的资料；图片上传仍走 HTTP。

`write_report` 固定写 AI 报告系统库。`library` 可省略（固定为 `ai-reports`），提供时仅接受该系统库 ID 或目录名，其他库报错。现有格式为标题、Markdown 正文及可选 `related_article_ids`；完整 AI 报告格式由 ikb-15 定义。

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"write_report","arguments":{"library":"ai-reports","title":"研究报告","markdown":"# 研究报告\n\n综合分析内容。"}}}
```

报告不审核、不进行模型处理，只保存并索引。同标题与正文的请求重试返回已有报告。保存后索引失败仍返回已保存结果及索引状态，待处理操作负责重试。
