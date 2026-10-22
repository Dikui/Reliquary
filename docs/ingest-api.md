# 收录 API

Reliquary 接受外部工具转换好的 Markdown 正文和图片，不抓取链接，不保存 PDF、Office 等原件。HTTP 与 MCP 共用收录服务。认证为 `Authorization: Bearer <API Key>`。先调用 MCP `list_libraries` 或 HTTP `GET /api/v1/libraries`，选择库的 `id` 或英文 `directory`；响应的 `accepted_fields` 说明该库接受的字段。

## 请求字段

`POST /api/v1/ingest` 接收 `{"items":[...]}`。单条请求使用一个元素，批量为 1 至 50 个元素。每项独立校验，缺库、库不存在或参数不合法时，该项返回 `status=invalid`，不影响其他有效项。

| 字段 | 规则 |
| --- | --- |
| `library` | 每项必填，库 ID 或英文目录名。不能用中文名称。缺少或不存在时提示调用 `list_libraries` |
| `content` | 非空可读 Markdown，可为草稿、片段或摘要；纯链接与仅图片不接收。正文与串文、引用合计上限 5,000,000 Unicode 码点 |
| `acquisition` | 可选，缺省表示未知；提供时为 `verbatim` 逐字原文、`extracted` 提取、`model_written` 模型整理 |
| `source_ref` | 所有模板接受。可选字符串，去首尾空白，最多 2048 Unicode 码点。同库内用于唯一定位来源 |
| `identifier` | 所有模板接受。可选 `scheme:value`，最多 2048 Unicode 码点。scheme 小写、DOI value 小写；其他 value 保留大小写 |
| `graph` | 可选布尔值；所有模板默认抽取有依据的关系，false 显式关闭关系抽取 |
| `recovery_failure_id` | 可选接收故障编号，用于关联修复请求与结果；不参与正文、来源身份或去重 |
| `url` | 可选，缺来源不阻止保存。提供时必须为 HTTP/HTTPS，最多 2048 字节 |
| `title` | 可选，最多 255 个码点；缺失不阻止保存，后台可补标题 |
| `author`、`publisher`、`published_at` | 可选来源信息；日期使用 YYYY-MM-DD 或 RFC3339 |
| `images` | 可选图片列表，最多 100 项，见下节 |
| `content_type_hint` | 可选分类提示，不替代处理结果 |
| `thread`、`quotes` | 可选有序正文片段与引用；每项 `content` 必填，合计正文仍受 500 万码点限制 |
| `collect_reason` | 收藏理由 |
| `agent_summary`、`agent_tags`、`source_name` | 调用方注释，保存数据库，不进入正文或处理提示词 |

`kind`、`document_id`、`file`、`entry_id`、`missing` 和未知字段被拒绝。所有模板均接受可选 source_ref 和 identifier。

技术故障的逐项结果可附带 `diagnostics: {failure_id, package_status, recorded}`；明确拒绝不保留故障包。`recorded=false` 表示未成功落盘。使用[接收故障查询接口](external-integration.md#接收故障与短期故障包)读取原始输入和图片，再通过本接口修正提交。`outcome=unknown` 时先核对现有资料与任务；`accepted` 表示正文、图片与后台任务已保存，不表示索引或模型处理已完成。

调用方应保留原文明确署名：`author` 填个人作者，`publisher` 填发布机构、媒体或平台账号，不用机构或转发账号替代文章作者，也不推测缺失的署名。正文保留作者观点和理解所需的回复、引用上下文。`verbatim` 不得删改主体；经过压缩、重述的材料应如实声明获取方式。提交原帖时保存原帖 URL，相关文章链接不能冒充原帖来源。客观机构新闻与外部个人观点按目标库用途分别收录。

所有模板共用入库、审核和处理规则。系统库目录仅为示例，实际可用库由 `list_libraries` 返回。source_ref、identifier、graph 不再按模板限制。

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

## 保存与同来源更新

入库只保留身份、格式、长度、图片与存储等技术校验。不逐条调用模型评价价值，不检查章节、文章完整度或最小篇幅；缺来源不进入常规待审。尚未明确的广告或无关内容排除规则不启用。已有 review_criteria 和来源证据配置不再作为新提交的准入门槛。

同库内按 source_ref、规范化 identifier 或规范化 URL 定位来源。唯一匹配且正文、实际图片与提供的元数据未变时返回 exists，不重复处理；有变化则覆盖旧文，返回同一 ID 和 accepted，撤除旧派生数据并重新处理。不保存正文版本。标识匹配不同资料时报告定位冲突。无可靠来源标识时新建，即使正文相同；不同库独立处理。不做入口近似判重或自动归并转载。

原有人工作出的拒收记录继续生效。已有来源未核实标记保留。任一已提交图片无效时整条回滚，已有正文不变。未提供的远程图片保留引用，不下载或生成缺图待办。

先保存正文、图片和元数据，再后台建立正文索引，随后运行摘要、分类、实体和图谱。空实体或关系结果合法；不为凑数编造事实。模型失败不回滚原文，也不撤销已建立的正文索引；向量暂不可用时保留全文检索并后台补算。parse_status 表示正文索引状态，summary_status 表示模型处理状态。前者 completed、后者 failed 表示正文仍可检索，模型处理需要重试。

## 更新已有资料的正文

网页资料详情的“编辑正文”提供完整 Markdown 编辑与预览。保存更新原资料，不创建新 ID；关闭、切换资料或离开页面前检查未保存修改。草稿仅保存在当前页面内存中，不保留旧正文版本。

`GET /api/v1/entries/:id/body` 的 `ETag` 为带双引号的完整正文 SHA-256；范围读取也返回同一校验值。HTTP 更新可提交 `base_body_hash`（去掉 ETag 的双引号），网页保存必须携带。后端在事务行锁内检查，正文已变化时返回 HTTP 409 和 `error.details.reason: "body_changed"`，不修改正文、附件或派生数据；相同正文的重试仍返回 `exists`。省略此字段的既有调用保持原更新语义。

HTTP `PUT /api/v1/entries/:id/body` 或 MCP `update_entry_content` 一起更新同一资料的完整 Markdown 正文和图片。认证与收录相同。资料必须已归档；保留资料 ID、库、来源、`source_ref`、`identifier`、创建时间、星标和用户主题。标题、摘要与自定义元数据仍通过 `PUT /api/v1/entries/:id` 修改。

新图片先通过 `POST /api/v1/ingest/assets` 上传，取得 `asset_id`，再与正文一起提交；也可直接在 `images[].data` 提交图片 base64/data URI。

```bash
curl -X PUT "$RELIQUARY_URL/api/v1/entries/$ENTRY_ID/body" \
  -H "Authorization: Bearer $RELIQUARY_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{"content":"# 项目计划\n\n更新后的完整正文。\n\n![图表](images/chart.png)","acquisition":"verbatim","images":[{"source_url":"images/chart.png","asset_id":"上传返回的 asset_id"}]}'
```

`content` 必填，不能只传链接或增量片段，最多 5,000,000 Unicode 码点；HTTP JSON 上限 64 MiB。`acquisition` 可选，省略时保留原值，所有模板均适用。MCP 参数为 `{"entry_id":"资料 ID","content":"完整 Markdown","acquisition":"verbatim"}`。

`images` 可选，最多 100 项。每项使用 `asset_id` 或 `data` 之一，并提供 `source_url`。该值必须与正文中的图片引用完全一致，可以是 HTTP/HTTPS 地址、`images/chart.png` 这类相对路径或本资料原有附件引用。同一引用只能提供一份图片；未被正文引用的图片会被拒绝。服务端将引用替换为本资料的本地附件路径，不抓取图片链接。内联图片每张最多 10 MiB、总计最多 20 MiB；上传附件沿用上传接口的大小限制。

正文、图片字节和获取方式均未变时返回 `exists`，不重复保存附件或处理。图片地址不变但字节变化也更新。成功返回原 `entry_id` 和 `accepted`；所有模板均后台重建正文索引及模型派生数据。通过 HTTP `GET /api/v1/entries/:id` 或 MCP `get_entry` 查询状态。保存失败时正文、JSON 与新增附件一起回滚；后台模型失败则保留已保存的新正文及已建索引。此接口不保留旧正文版本。

未归档、正在处理或删除、所属库有未完成任务时返回 HTTP 409，等待完成或取消任务后重试。数据库写入或任务提交失败时，数据库、Markdown 文件和本次新增附件一起回滚，用户上传的源附件仍可重试使用。此接口不保留旧正文版本，也不通过删除再收录实现更新，不产生丢弃指纹。

正文可直接保留本资料已有附件的相对引用，无须重复上传。提供新图片时创建独立附件，不覆盖原文件。未提供图片文件的远程链接保留原引用，不生成待办；未提供文件的其他本地引用会被拒绝。

`ingest` 也可按同库唯一来源标识覆盖原资料；指定 ID 的正文更新接口保留现有来源标识与创建时间。

## 来源标识与结果

source_ref、identifier 和规范化 URL 用于同库精确定位，具体见上文。新提交不再追加同来源的正文版本。既有 has_update、latest_entry_id 字段继续返回历史记录关系；不批量迁移旧记录。多个历史记录造成来源定位歧义时明确报错。

逐项结果另含 `index`、`library_id`、`entry_id`、`status`、`message`、`reason`、`image_failures`。正常提交返回 `accepted`、`exists`、`discarded` 或 `invalid`；不生成常规 `pending_review`。无效项原因可包含：

| 错误 | 返回方式与信息 |
| --- | --- |
| 缺库或库不存在 | HTTP 200 的对应项 `invalid`，原因包含 `library` 与 `call list_libraries` |
| 标识冲突 | 对应项 `invalid`，说明多个来源标识匹配不同资料，不猜测覆盖目标 |
| 正文缺失或超长、标识格式无效 | 对应项 `invalid`，或流式读取时立即拒绝超长请求；给出具体字段与上限 |
| JSON 语法、批量包络或字节上限错误 | HTTP 400，应用错误码 `1000`；MCP 返回工具错误，流式读取超限为带具体原因的 HTTP 400 |

`POST /api/v1/ingest/check` 接收 `{"library":"official","urls":["https://example.org/release"]}`，每次 1 至 100 个 URL。所有模板均只在指定库查询。返回 in_library、历史 pending_review、not_found、discarded 或 invalid。该接口只查来源存在性；不能据此跳过同来源内容更新，仍应提交 ingest 比较正文和图片。

## 图片提交

先上传图片：

```sh
curl -X POST "$BASE/api/v1/ingest/assets" \
  -H "Authorization: Bearer $KEY" -F 'file=@chart.png'
```

上传只接受真实图片，支持 png、jpg、jpeg、bmp、tiff、webp，不接受 GIF 或 SVG。附件上传与内联 Base64 均按实际图片内容检查，修改扩展名或 data URI 的 MIME 类型不能绕过 GIF 限制。用返回的 `asset_id`，或在 `data` 中提供 base64/data URI；两者不能同时提供。`source_url` 对应正文中的原图片地址。

原图最多 2 亿像素，并须通过解码内存预算检查；超限时返回明确错误，要求调用方缩小后重新提交。文件体积限制仍按下文执行。原图字节和正文引用不会因模型适配被覆盖或删除。历史 GIF 保留原件，重新处理时报告不支持，需调用方替换成静态图片。

审核、摘要、图片向量及重排统一按需生成模型输入副本：最长边不超过 4096 像素、总像素不超过 800 万、编码后不超过 8 MiB。合格 JPEG、PNG、WebP 保持原字节；其他图片等比缩放并优先编码 PNG，超限时转 JPEG，透明区域合成到白底。副本不长期另存。图片预处理串行执行，解码前检查约 1.5 GiB 的工作集预算。永久图片输入错误不自动重试；网络故障和限流继续使用原有重试。

图片可在首次收录时通过 `images` 提交。正文中的远程图片引用也可直接保留，不需要单独声明缺失原因。服务端不下载远程图片。

已有资料需要增加或替换图片时，使用上文的正文更新接口，一并提交完整正文和图片。`ingest` 按来源标识新建或覆盖，不再接受 `entry_id` 或 `missing`。缺图清单、接受缺失的 HTTP 接口与 MCP 工具已移除。

## 长度和传输上限

正文安全上限为 5,000,000 Unicode 码点。中文通常占 15,000,000 UTF-8 字节，全部四字节字符为 20,000,000 字节。单项 JSON 上限 64 MiB，即 67,108,864 字节，容纳正文与图片传输开销。批量 JSON 总上限 128 MiB，即 134,217,728 字节；MCP HTTP 消息含协议包络上限 129 MiB，即 135,266,304 字节。即使每项均未超限，批量也可能超限，应拆批。

内联图片每张解码后最多 10 MiB，总计最多 20 MiB；20 MiB 的 base64 约 26.7 MiB。图片上传还受部署的上传限制。流式读取器在读到正文码点或总字节上限时停止，报错而不截断，不读取剩余无限数据。不要依赖 Content-Length 检查代替流式限制。

## MCP

`ingest` 使用同样的 `items` 包络、字段与逐项状态。例如：

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ingest","arguments":{"items":[{"library":"personal","source_ref":"notes/mcp.md","content":"# 笔记\n\n通过 MCP 收录的正文。","acquisition":"verbatim","graph":false}]}}}
```

先 `initialize`，随后请求 `/mcp`，使用 Bearer API Key、`Accept: application/json, text/event-stream` 和协商的 `MCP-Protocol-Version`。`list_libraries` 返回库用途、模板、审核和来源说明、去重说明及 `accepted_fields`。`check_urls` 参数与 HTTP 相同。图片上传走 HTTP，已有资料的正文和图片更新使用 `update_entry_content`。

## AI产出收录

AI产出和其他库共用 HTTP `POST /api/v1/ingest` 与 MCP `ingest`。每项指定 `library: "ai-output"` 或对应库 ID，提交 `title`、`content` 与 `acquisition: "model_written"`：

```json
{"items":[{"library":"ai-output","title":"研究报告","content":"# 研究报告\n\n综合分析内容，引用资料链接写在正文中。","acquisition":"model_written"}]}
```

MCP 将同一对象作为 `ingest` 的 `arguments`。字段和错误按上文通用收录契约处理。来源资料可在正文中引用；收录不接受专用的关联资料、实体、主题或 `request_id` 参数。

生成型同样先保存，再后台运行正文索引、摘要、分类、实体和图谱。保留 `cannot_be_fact=true`，处理成功不代表陈述已核实。acquisition 仍可省略；无来源标识时相同正文也正常新建。

## 长文处理

正文及串文、引用合计上限 5,000,000 Unicode 码点。保存不等待模型分段、审核或摘要。超过模型上下文、分段上限或输出格式错误，只记录后台处理失败，不转为内容待审，不撤销原文及已建立的正文检索。

后台沿用 Markdown 分段、检查点和重试机制。各段摘要合并、内容类型选择、实体归一与证据校验继续执行。摘要要点按实际内容确定，不要求固定 3–5 条。标题缺失在处理调用中补写，不修改正文。

配置见 [个人使用指南](personal-use.md)。默认最多 128 段，每篇顺序执行，受现有模型并发限制。

## 联合处理的图片输入

所有模板均选择所属库内容类型、补全缺失标题、生成摘要与主题，并抽取有依据的实体、短介绍和关系。默认运行图谱，`graph:false` 显式关闭关系抽取；内容类型不再关闭图谱。内容类型修正仍只能选择所属库启用的类型。

支持视觉的处理模型读取由已保存原图生成的模型输入副本，将重要视觉信息纳入理解和摘要。默认处理不再要求、校验或保存逐图说明，也不生成新的 `image_description` 检索块；模型额外返回的 `image_descriptions` 不参与处理结果。每段最多读取 `article_processing.max_images` 张图片，超过时增加分段。图片不可读或模型不支持视觉时保存跳过原因。

资料详情沿用 `image_descriptions` 字段返回图片引用和读取状态，包括 `url`、`ordinal` 和 `locations`，有历史说明或跳过原因时附带 `description`、`skip_reason`。新处理结果不返回 `description`。`ordinal` 按正文引用顺序编号，同一图片出现多次时分别记录。图片引用和位置保留，历史说明仍可读取，不批量清理。历史说明命中仍可返回 `chunk_type: "image_description"`；位置使用保存后的 Markdown 正文坐标。关系及来源通过实体详情和 [关系接口](relations-api.md) 读取。

原图仍可建立图片向量，需要支持图片输入的 embedding 配置。图片命中后，`qwen3-vl-rerank` 可通过多模态接口读取窗口内每条候选最高匹配图片的模型输入副本和文章文本，取该条文本与图像得分的最大值排序。不支持图像重排、附件读取失败或模型失败时保留融合顺序；文本候选继续使用文本重排。多模态重排按融合顺序截取最多 40 个文档项作为单次窗口：每条资料占一个文本项，命中图片再占一个原图项，因此窗口至少容纳前 20 条资料（候选不足时全部纳入）。仅窗口内排序，其余候选保留原位置和顺序，不跨请求比较分数。此行为不新增配置，也不批量回填历史资料。
