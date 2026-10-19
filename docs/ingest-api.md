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
| `identifier` | 仅外部长效型库接受。可选 `scheme:value`，最多 2048 Unicode 码点。去两部分首尾空白，scheme 小写，DOI 的 value 也小写。其他 scheme 的值保留大小写。在所属库内参与去重 |
| `graph` | 可选布尔值，保存在资料上；未提供或 null 表示按库和内容类型默认。仅外部时效型库在处理时依该值和内容类型开关抽取图谱；自有型、外部长效型和生成型库不抽图谱，即使设为 true |
| `entry_id` | 仅补全使用，已有资料 ID。HTTP 与 MCP 均使用 `entry_id`，不接受旧字段别名 |
| `recovery_failure_id` | 可选接收故障编号，用于关联修复请求与结果；不参与正文、来源身份或去重 |
| `url` | 可选来源链接，可为空；非空必须为 HTTP 或 HTTPS，最多 2048 字节。现有外部来源审核仍可能要求链接 |
| `title` | 可选标题，最多 255 个码点；AI产出需要标题 |
| `author`、`publisher`、`published_at` | 可选来源信息；日期使用 YYYY-MM-DD 或 RFC3339 |
| `images` | 可选图片列表，最多 100 项，见下节 |
| `missing` | 仅图片缺失：`type=image`、`source_url` 和非空 `reason` |
| `content_type_hint` | 可选分类提示，不替代处理结果 |
| `thread`、`quotes` | 可选有序正文片段与引用；每项 `content` 必填，合计正文仍受 500 万码点限制 |
| `collect_reason` | 收藏理由 |
| `agent_summary`、`agent_tags`、`source_name` | 调用方注释，保存数据库，不进入正文或处理提示词 |

`kind`、`document_id`、`file` 和未知字段被拒绝。向其他模板提交空字符串 `source_ref` 或 `identifier` 也会被拒绝。

技术故障的逐项结果可附带 `diagnostics: {failure_id, package_status, recorded}`；明确拒绝不保留故障包。`recorded=false` 表示未成功落盘。使用[接收故障查询接口](external-integration.md#接收故障与短期故障包)读取原始输入和图片，再通过本接口修正提交。`outcome=unknown` 时先核对现有资料与任务；`accepted` 仍只表示已接收。

| 系统库目录 | 模板 | 专有可选字段 |
| --- | --- | --- |
| `official`，官方信息 | `external_timely` | 无 |
| `media`，媒体信息 | `external_timely` | 无 |
| `references`，参考文档 | `external_durable` | `identifier` |
| `personal`，个人产出 | `own` | `source_ref` |
| `ai-output`，AI产出 | `generated` | 无 |

自建库按模板接受相同字段。`graph` 五个库都可保存。除生成型外，均按所属库的审核标准审核，并按模板验证来源证据、去重和处理。生成型收录调用报告保存与索引流程，不审核、不调用处理模型。五个系统库和自建库统一通过 HTTP `POST /api/v1/ingest` 或 MCP `ingest` 收录。

## 五个系统库示例

以下请求均发送到 `/api/v1/ingest`。例如：

```sh
curl -X POST "$BASE/api/v1/ingest" \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  --data-binary '{"items":[{"library":"official","url":"https://example.org/release","title":"机构公告","publisher":"示例研究院","content":"# 公告\n\n完整公告正文与测量方法。","acquisition":"extracted","graph":true}]}'
```

媒体信息：

```json
{"items":[{"library":"media","url":"https://example.org/interview","title":"访谈观点","author":"张三","publisher":"示例访谈","content":"# 访谈\n\n完整访谈记录。","acquisition":"verbatim"}]}
```

参考文档：

```json
{"items":[{"library":"references","url":"https://example.org/paper","title":"实验论文","publisher":"示例期刊","identifier":"DOI:10.1234/EXAMPLE","content":"# 论文\n\n完整研究方法与结果。","acquisition":"extracted"}]}
```

个人产出：

```json
{"items":[{"library":"personal","source_ref":"notes/calibration.md","title":"校准记录","content":"# 校准\n\n仪器读数为 42。","acquisition":"verbatim","graph":false}]}
```

AI产出：

```json
{"items":[{"library":"ai-output","title":"研究报告","content":"# 研究报告\n\n综合分析内容。","acquisition":"model_written","graph":false}]}
```

## 审核、去重和补全

审核提示词使用资料所属库的 `review_criteria`，结果与原因写入收录审核记录。生成型不调用审核或处理模型，只保存并建立索引。本文规则适用于系统库和用户按模板新建的库；显示名称与目录名不决定行为。

审核模型同时读取 Markdown 正文与已提交的可读图片，结合图文应用同一套收录标准，图片中的信息不必重复写成文字。每张图片带本段内的编号、正文位置（Unicode 码点）、是否附带图片本体和读取失败原因；附件按可读图片的顺序发送，跳过项保留原编号。审核分段同时受正文预算和现有单次图片数量上限约束。模型调用记录保存已发送图片的路径与 SHA-256。

| 模板 | 来源证据 | 本库内识别同一份资料 |
| --- | --- | --- |
| 外部时效型，`link_required` | 必须有原文链接；缺失进入收录待审，原因“缺少原文链接” | 规范化 URL、正文哈希、同标题近似正文；没有链接时，还按机构、标题、有效发布日期识别 |
| 外部时效型，`link_or_publisher_date` | 有链接正常审核；无链接但有发布者和有效发布日期可审核入库，并标记 `source_unverified=true`；缺少任一项进入收录待审 | 同上 |
| 外部长效型 | 链接或 `identifier` 至少一个，否则进入收录待审 | 规范化 URL、规范化 `identifier`、正文哈希；不做近似去重 |
| 自有型 | 不要求来源证据 | `source_ref` 优先，按最新同来源正文比较；没有 `source_ref` 时按正文哈希 |
| 生成型 | 不要求来源证据 | 不做来源与近似去重；报告写入按请求 ID 或内容指纹幂等 |

同一链接或正文可以收入不同库。确认不收录后，仅在原库保存链接、正文、标识或来源身份的适用指纹，再次提交相同指纹返回 `discarded`。自有型有 `source_ref` 时，丢弃指纹同时包含该来源与正文，正文变化仍可重新收录。

只有外部时效型归并转载来源和折叠相似报道。检索、资料列表及关注动态仅折叠同库、同向量模型且满足时间窗口和相似度阈值的报道；外部长效型、自有型、生成型不折叠。资料列表可用 `collapse_similar=false` 展开；默认返回代表资料和 `similar_reports`，先折叠再分页。

来源未核实标记在 HTTP/MCP 收录结果、资料列表、详情、正文读取、检索和关注动态返回中保留。网页沿用已有标记位置。缺链接的待审条目继续支持补链接并重新审核、明确确认无链接入库、确认不收录。补上链接后清除未核实标记；无链接手工入库仍标记未核实。

图片缺失继续标记为“缺少图片”。调用方按 `library` 与 `entry_id` 定位原资料，补全成功后重新处理，撤换旧关系证据并重新计算正文位置；缺少图片与来源证据不足分别处理。长文按文末规则分段审核。

## 更新已有资料的正文

HTTP `PUT /api/v1/entries/:id/body` 或 MCP `update_entry_content` 一起更新同一资料的完整 Markdown 正文和图片。认证与收录相同。资料必须已归档；保留资料 ID、库、来源、`source_ref`、`identifier`、创建时间、星标和用户主题。标题、摘要与自定义元数据仍通过 `PUT /api/v1/entries/:id` 修改。

新图片先通过 `POST /api/v1/ingest/assets` 上传，取得 `asset_id`，再与正文一起提交；也可直接在 `images[].data` 提交图片 base64/data URI。

```bash
curl -X PUT "$RELIQUARY_URL/api/v1/entries/$ENTRY_ID/body" \
  -H "Authorization: Bearer $RELIQUARY_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"content":"# 项目计划\n\n更新后的完整正文。\n\n![图表](images/chart.png)","acquisition":"verbatim","images":[{"source_url":"images/chart.png","asset_id":"上传返回的 asset_id"}]}'
```

`content` 必填，不能只传链接或增量片段，最多 5,000,000 Unicode 码点；HTTP JSON 上限 64 MiB。`acquisition` 可选，省略时保留原值；生成型只能使用 `model_written`。MCP 参数为 `{"entry_id":"资料 ID","content":"完整 Markdown","acquisition":"verbatim"}`。

`images` 可选，最多 100 项。每项使用 `asset_id` 或 `data` 之一，并提供 `source_url`。该值必须与正文中的图片引用完全一致，可以是 HTTP/HTTPS 地址、`images/chart.png` 这类相对路径或本资料原有附件引用。同一引用只能提供一份图片；未被正文引用的图片会被拒绝。服务端将引用替换为本资料的本地附件路径，不抓取图片链接。内联图片每张最多 10 MiB、总计最多 20 MiB；上传附件沿用上传接口的大小限制。

正文、图片字节和获取方式均未变时返回 `exists`，不重复保存附件或处理。图片地址不变但字节变化也会执行更新。更新成功返回原 `entry_id` 和 `accepted`，表示正文及后台任务已保存；通过 HTTP `GET /api/v1/entries/:id` 或 MCP `get_entry` 查看处理状态。非生成型重新审核并生成摘要、索引及适用的图谱关系；生成型只重建索引。旧索引、模型主题、实体关联和关系来源立即撤除，处理期间不继续返回旧正文的检索结果。审核不通过时保留新正文并进入收录待审；处理失败可按现有审核或重新处理流程重试。

未归档、正在处理或删除、所属库有未完成任务时返回 HTTP 409，等待完成或取消任务后重试。数据库写入或任务提交失败时，数据库、Markdown 文件和本次新增附件一起回滚，用户上传的源附件仍可重试使用。此接口不保留旧正文版本，也不通过删除再收录实现更新，不产生丢弃指纹。

正文可直接保留本资料已有附件的相对引用，无须重复上传。提供新图片时创建独立附件，不覆盖原文件。非生成型正文中未提供图片文件的远程链接仍标记缺图，之后可用现有 `ingest` 补全；未提供文件的其他本地引用会被拒绝。

原有 `ingest` 的规则不变：自有型同一 `source_ref` 提交变化正文仍新增记录；只有本节接口会修改原记录。修改同来源的历史记录不会改变创建时间，也不会使它成为最新记录。

## 来源标识与结果

自有型库同一 `source_ref` 的正文，按规范 Markdown（含追加片段、引用和图片稳定标识）去首尾空白、统一空白后计算 SHA-256，与最新一条比较。相同则返回 `exists` 和已有 `entry_id`；不同则保存新资料，保留旧记录。再次提交较早的正文也会产生新记录。无 `source_ref` 时按正文哈希去重；不同库互不影响。

资料详情、HTTP 收录结果、MCP 收录与 `get_entry` 返回 `source_ref`、`identifier`、`graph`、`source_unverified`、`has_update`、`latest_entry_id`。`has_update=true` 表示存在更新记录，`latest_entry_id` 指向同库同来源最新的未删除记录。最新记录本身返回 false 与自己的 ID；无来源标识时不提供最新 ID。历史记录的查询、正文与时间保持不变。

逐项结果另含 `index`、`library_id`、`entry_id`、`status`、`message`、`reason`、`image_failures`、`completeness`。状态包括 `accepted`、`exists`、`supplemented`、`pending_review`、`conflict`、`discarded`、`invalid`。无效项原因可包含：

| 错误 | 返回方式与信息 |
| --- | --- |
| 缺库或库不存在 | HTTP 200 的对应项 `invalid`，原因包含 `library` 与 `call list_libraries` |
| 字段不适用于库 | 对应项 `invalid`，原因包含 `source_ref is only accepted by own libraries` 或 `identifier is only accepted by external_durable libraries` |
| 错误、跨库或未标记缺少图片的 ID | 对应项 `invalid`，原因分别包含 `does not identify an existing entry`、`different library`、`not awaiting image completion` |
| 正文缺失或超长、标识格式无效 | 对应项 `invalid`，或流式读取时立即拒绝超长请求；给出具体字段与上限 |
| JSON 语法、批量包络或字节上限错误 | HTTP 400，应用错误码 `1000`；MCP 返回工具错误，流式读取超限为带具体原因的 HTTP 400 |

`POST /api/v1/ingest/check` 接收 `{"library":"official","urls":["https://example.org/release"]}`。每次 1 至 100 个链接；缺库或库不存在时每个链接返回 `invalid` 和原因。已存在结果为 `in_library` 或 `pending_review`；其他状态为 `not_found`、`discarded`。自有型资料不按链接判重。生成型也不按链接判重。所有查询仅在指定库生效。

## 图片与按资料 ID 补全

先上传图片：

```sh
curl -X POST "$BASE/api/v1/ingest/assets" \
  -H "Authorization: Bearer $KEY" -F 'file=@chart.png'
```

上传只接受真实图片，支持 png、jpg、jpeg、bmp、tiff、webp，不接受 GIF 或 SVG。附件上传与内联 Base64 均按实际图片内容检查，修改扩展名或 data URI 的 MIME 类型不能绕过 GIF 限制。用返回的 `asset_id`，或在 `data` 中提供 base64/data URI；两者不能同时提供。`source_url` 对应正文中的原图片地址。

原图最多 2 亿像素，并须通过解码内存预算检查；超限时返回明确错误，要求调用方缩小后重新提交。文件体积限制仍按下文执行。原图字节和正文引用不会因模型适配被覆盖或删除。历史 GIF 保留原件，重新处理时报告不支持，需调用方替换成静态图片。

审核、摘要、图片向量及重排统一按需生成模型输入副本：最长边不超过 4096 像素、总像素不超过 800 万、编码后不超过 8 MiB。合格 JPEG、PNG、WebP 保持原字节；其他图片等比缩放并优先编码 PNG，超限时转 JPEG，透明区域合成到白底。副本不长期另存。图片预处理串行执行，解码前检查约 1.5 GiB 的工作集预算。永久图片输入错误不自动重试；网络故障和限流继续使用原有重试。

首次收录缺图片：

```json
{"items":[{"library":"personal","source_ref":"notes/figure.md","title":"测量记录","content":"# 测量\n\n完整正文。\n\n![图表](https://example.org/chart.png)","acquisition":"verbatim","missing":[{"type":"image","source_url":"https://example.org/chart.png","reason":"暂未取得图片"}]}]}
```

补全只凭库与资料 ID 定位，无须 URL 或再次提交正文：

```json
{"items":[{"library":"personal","entry_id":"首次收录返回的 entry_id","images":[{"asset_id":"上传返回的 asset_id","source_url":"https://example.org/chart.png"}]}]}
```

补全把正文中匹配的图片引用替换为本地路径，文字保持原样。可选 `content` 用于旧有正文冲突检查；不同正文另存关联收录待审，返回 `conflict`，不覆盖原资料。保留原有规范化与近似正文比较规则。不能用相同 URL 自动补全。图片替换后重新计算保存正文的哈希和位置，检索与关系证据使用更新后的坐标。

`GET /api/v1/ingest/incomplete?page=1&page_size=20` 列出缺少图片的资料，含 `entry_id`、`library_id` 与缺失图片。`include_attempted=false` 排除已尝试补全一次的资料。`POST /api/v1/ingest/:id/accept-incomplete` 接受缺失，`POST /api/v1/ingest/:id/discard` 丢弃资料，可附 `reason`。

## 长度和传输上限

正文安全上限为 5,000,000 Unicode 码点。中文通常占 15,000,000 UTF-8 字节，全部四字节字符为 20,000,000 字节。单项 JSON 上限 64 MiB，即 67,108,864 字节，容纳正文与图片传输开销。批量 JSON 总上限 128 MiB，即 134,217,728 字节；MCP HTTP 消息含协议包络上限 129 MiB，即 135,266,304 字节。即使每项均未超限，批量也可能超限，应拆批。

内联图片每张解码后最多 10 MiB，总计最多 20 MiB；20 MiB 的 base64 约 26.7 MiB。图片上传还受部署的上传限制。流式读取器在读到正文码点或总字节上限时停止，报错而不截断，不读取剩余无限数据。不要依赖 Content-Length 检查代替流式限制。

## MCP

`ingest` 使用同样的 `items` 包络、字段与逐项状态。例如：

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ingest","arguments":{"items":[{"library":"personal","source_ref":"notes/mcp.md","content":"# 笔记\n\n通过 MCP 收录的正文。","acquisition":"verbatim","graph":false}]}}}
```

先 `initialize`，随后请求 `/mcp`，使用 Bearer API Key、`Accept: application/json, text/event-stream` 和协商的 `MCP-Protocol-Version`。`list_libraries` 返回库用途、模板、审核和来源说明、去重说明及 `accepted_fields`。`check_urls` 参数与 HTTP 相同。`list_incomplete`、`accept_incomplete` 可用于发现和处理缺图片的资料；图片上传仍走 HTTP。

## AI产出收录

AI产出和其他库共用 HTTP `POST /api/v1/ingest` 与 MCP `ingest`。每项指定 `library: "ai-output"` 或对应库 ID，提交 `title`、`content` 与 `acquisition: "model_written"`：

```json
{"items":[{"library":"ai-output","title":"研究报告","content":"# 研究报告\n\n综合分析内容，引用资料链接写在正文中。","acquisition":"model_written"}]}
```

MCP 将同一对象作为 `ingest` 的 `arguments`。字段和错误按上文通用收录契约处理。来源资料可在正文中引用；收录不接受专用的关联资料、实体、主题或 `request_id` 参数。

生成型资料不审核，不调用内容处理模型，不抽图谱，只建立 BM25 与向量索引；向量索引仍会调用 embedding。资料保存后带 `cannot_be_fact=true`，不能当作事实。索引失败保留原文并由后台任务重试。相同标题与正文的重试按内容指纹返回已有资料，状态为 `exists`；新保存返回 `accepted`。

## 长文审核与处理

正文安全上限仍为 5,000,000 个 Unicode 码点。实际可自动处理的长度还受所选模型上下文与单篇分段数限制，超过限制的资料进入收录待审，原因中包含分段上限，不会无限调用模型或截断后标记完成。

长文返回 `accepted` 表示资料、分段计划和待处理任务已提交，随后在后台完成逐段审核。审核尚未通过时不会写入文件库；调用方可用资料 ID 查询处理状态，某段被拒绝后状态进入收录待审。短文仍在当前收录调用中审核一次。

审核使用所属库的轻量模型与审核标准，内容处理使用生成模型。超过一次输入预算时，按 Markdown 标题和段落边界分段，表格和代码块优先保持完整；单个块超过预算才按字符切开，并附上少量前文。每段带标题路径及正文字符范围。任何一段明确不通过，整篇进入收录待审，原因列出段号；全部通过才收录。生成型库继续免审核、免内容处理。

处理逐段生成摘要、内容类型、实体和主题。长文额外调用一次生成模型合并所有段摘要；内容类型采用多数票，同票取最早出现的类型。实体按现有规则归入全局档案，主题按 ID 去重，结果保留来源段。若各段摘要超出合并上下文，会明确失败并保留检查点，不会截掉摘要后完成。

标题为空、等于来源链接（含截断至 255 字符的链接），或为默认名称“待审文章”“本人文档”时，处理模型在生成摘要的同次调用中补写标题；长文在摘要合并调用中根据全部段摘要生成整篇标题。不增加单独的标题生成请求，已有有效标题保留。生成标题用于资料显示、检索和文件表头，不改正文；已有文件路径保留。历史资料在下次重新处理时适用，不自动批量重跑。

服务端在 `entry_segment_runs` 保存审核与处理各自的分段计划、范围、哈希、逐段状态和结果。模型失败或进程退出后，重试只调用未完成段；已经完成的摘要合并也可复用。正文、模型输入配置或分段规则变化会新建完整计划。只有全部段及合并成功后才进入后续保存和索引，部分处理不会显示为完成。处理时间线显示“第 3/12 段”等进度。

配置见 [个人使用指南](personal-use.md) 的长文配置。默认最多 128 段，每篇顺序执行，仍受现有模型并发限制。

## 联合处理的图片输入

除 AI产出外，处理调用从所属库的内容类型清单选择类型，补全缺失标题并输出摘要和主题。仅外部时效型库抽取实体、短介绍和关系，`graph: true/false` 覆盖内容类型的图谱开关；未提供时使用所属库的配置，其他默认不抽关系。自有型、外部长效型库不抽实体、短介绍和关系，即使设为 `graph: true`。资料的内容类型修正只能选择所属库启用的类型。

支持视觉的处理模型读取由已保存原图生成的模型输入副本，将重要视觉信息纳入理解和摘要。默认处理不再要求、校验或保存逐图说明，也不生成新的 `image_description` 检索块；模型额外返回的 `image_descriptions` 不参与处理结果。每段最多读取 `article_processing.max_images` 张图片，超过时增加分段。图片不可读或模型不支持视觉时保存跳过原因。

资料详情沿用 `image_descriptions` 字段返回图片引用和读取状态，包括 `url`、`ordinal` 和 `locations`，有历史说明或跳过原因时附带 `description`、`skip_reason`。新处理结果不返回 `description`。`ordinal` 按正文引用顺序编号，同一图片出现多次时分别记录。图片引用和位置保留，历史说明仍可读取，不批量清理。历史说明命中仍可返回 `chunk_type: "image_description"`；位置使用保存后的 Markdown 正文坐标。关系及来源通过实体详情和 [关系接口](relations-api.md) 读取。

原图仍可建立图片向量，需要支持图片输入的 embedding 配置。图片命中后，`qwen3-vl-rerank` 可通过多模态接口读取窗口内每条候选最高匹配图片的模型输入副本和文章文本，取该条文本与图像得分的最大值排序。不支持图像重排、附件读取失败或模型失败时保留融合顺序；文本候选继续使用文本重排。多模态重排按融合顺序截取最多 40 个文档项作为单次窗口：每条资料占一个文本项，命中图片再占一个原图项，因此窗口至少容纳前 20 条资料（候选不足时全部纳入）。仅窗口内排序，其余候选保留原位置和顺序，不跨请求比较分数。此行为不新增配置，也不批量回填历史资料。
