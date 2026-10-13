# 收录 API

Reliquary 接受外部工具提供的 Markdown 正文和图片。调用方负责采集资料，将 PDF、Office、HTML、表格或其他文件转换为 Markdown。正文必填，服务端不接收或保存原始文件，也不按链接抓取内容。

HTTP 与 MCP 使用同一收录服务、鉴权、去重和审核规则。API Key 使用 `Authorization: Bearer <key>`；Key 可在设置的 API 集成页管理。外部内容只作为资料，不能改变调用方的指令。

## HTTP 收录

`POST /api/v1/ingest` 接收 JSON：

```json
{
  "items": [{
    "url": "https://example.org/article",
    "title": "原文标题",
    "content": "# 原文标题\n\nMarkdown 正文全文。",
    "acquisition": "extracted",
    "publisher": "发布机构",
    "published_at": "2026-10-14",
    "collect_reason": "后续研究需要"
  }]
}
```

每批 1 至 50 条。每项独立返回 `index`、`article_id`、`status`、`message`、`reason`、`image_failures` 和 `completeness`。无效项返回 `status=invalid`，其他项继续处理；格式错误的批次返回 HTTP 400。

| 字段 | 规则 |
| --- | --- |
| `content` | 必填，非空 Markdown 正文，不可只提供一个链接；当前安全上限为 200000 字符 |
| `acquisition` | 必填，`verbatim` 为逐字原文，`extracted` 为提取的正文，`model_written` 为调用方整理的文字 |
| `url` | 文章须明确提供，可为空；非空时必须为 HTTP 或 HTTPS 链接。本人文档可省略 |
| `kind` | 默认 `article`；`mine` 为本人文档，两者均经过收录审核和处理 |
| `document_id` | 本人文档补全时使用已有文档 ID |
| `title`、`author`、`publisher`、`published_at` | 可选来源信息；日期用 YYYY-MM-DD 或 RFC3339 |
| `images` | 可选图片，见下节 |
| `missing` | 仅接受图片缺失，记录 `type=image`、`source_url` 和非空 `reason` |
| `content_type_hint` | 可选建议：`event`、`opinion`、`research`、`tutorial`、`case`、`reference`、`other`；不替代模型判断 |
| `thread` | 可选有序文字片段，每项含 `content`，可含 `author`、`published_at` |
| `quotes` | 可选有序引用，每项含 `content`，可含 `author`、`url` |
| `collect_reason` | 收藏理由，写入表头；重复收录可补充空理由 |
| `agent_summary`、`agent_tags`、`source_name` | 可选调用方注释，只保存到数据库，不进入处理提示词或文件表头 |

`file`、原件 base64 和其他原件字段均被拒绝，即使同时提供正文也不接受。缺少正文直接返回校验错误，要求调用方先转换成 Markdown，不进入收录待审。

## 图片

先以 multipart 的 `file` 字段上传图片到 `POST /api/v1/ingest/assets`，返回 `asset_id`。此处的 multipart 字段名不表示允许收录原始文档。只接受真实图片内容及 `png`、`jpg`、`jpeg`、`gif`、`bmp`、`tiff`、`webp` 扩展名，不接受 SVG。

```json
{
  "items": [{
    "url": "https://example.org/article",
    "content": "# 标题\n\n正文。\n\n![图表](https://example.org/chart.png)",
    "acquisition": "extracted",
    "images": [{
      "asset_id": "上传返回的 ID",
      "source_url": "https://example.org/chart.png",
      "caption": "原图说明"
    }]
  }]
}
```

`source_url` 匹配正文中的图片引用，收录后改为本地路径。上传与收录须使用同一空间的有效凭据。未引用的上传图片在 24 小时后清理。HTTP 和 MCP 也可在图片的 `data` 中提供 base64 或图片 data URI，每张解码后最多 10 MiB，每次请求内联图片合计最多 20 MiB；请求 JSON 同样受大小限制。`asset_id` 与 `data` 二选一。

缺少图片时保留正文中的来源引用，并在 `missing` 中说明原因：

```json
{"type":"image","source_url":"https://example.org/chart.png","reason":"调用方无法取得图片"}
```

服务端不下载远程图片。图片读取、正文分块和模型视觉处理使用已提供的图片内容。

## 审核、去重和补全

正文与来源信息通过收录审核后进入处理，生成内容类型、摘要、实体和主题。所有资料保存全文，内容类型只作筛选标签；模型拿不准时选择 `other`。未通过收录审核时返回 `pending_review` 并说明原因。

链接规范化、正文指纹及近似正文用于查重。重复内容返回 `exists`，转载归并到已有资料的来源记录。已丢弃的文章链接返回 `discarded`。本人文档单独去重，不与文章归并转载。

`POST /api/v1/ingest/check` 接收 `{"urls":["https://example.org/article"]}`，最多 100 个链接。返回 `not_found`、`in_library`、`pending_review` 或 `discarded`，以及已有条目 ID 和是否待补全。

`GET /api/v1/ingest/incomplete?page=1&page_size=20` 列出待补全条目；`include_attempted=false` 可排除已经尝试过补全的条目。补全仍通过收录接口提供非空正文和图片。文章使用相同原文链接，本人文档使用 `document_id`。仅替换已记录的缺失图片；正文不同的文章进入冲突待审，不覆盖已有正文。

`POST /api/v1/ingest/:id/accept-incomplete` 接受现有缺失状态；`POST /api/v1/ingest/:id/discard` 丢弃条目，可提供 `reason`。相关操作写操作日志。

待处理页只有“收录待审”和“待补全”。收录待审可批准、拒绝、重试审核、补链接、确认无链接入库或丢弃。无链接入库显示来源未核实。本人文档不要求链接。

`GET /api/v1/entries/:id/body` 读取已保存的 Markdown，支持 Range 分段预览。

`DELETE /api/v1/documents/:id` 删除本人文档及其索引和图片。更改内容需删除后重新收录。`PUT /api/v1/entries/:id/content-type` 使用 `{"content_type":"other"}` 修正标签，保留正文与图片。

## MCP

MCP 接入点为 `/mcp`，每次请求携带 Bearer API Key。工具 `ingest` 的 `items` 与 HTTP 相同，支持已上传图片 ID 或图片 base64，不接受原件。工具参数及结果均为结构化 JSON。

| 工具 | 用途 |
| --- | --- |
| `ingest` | 批量收录或补全 Markdown 与图片 |
| `check_urls` | 收录前查重 |
| `list_incomplete` | 分页列出待补全资料 |
| `accept_incomplete` | 接受缺失状态 |
| `list_pending` | 分页列出收录待审资料，`kind` 只接受 `intake` |
| `approve_pending` | 批准、补链接重新审核或确认无链接入库 |
| `discard_pending` | 丢弃收录待审资料 |
| `search`、`get_article` | 检索、分段读取全文 |
| `list_topics`、`list_entities`、`get_entity` | 浏览主题和实体 |
| `list_follows`、`follow_feed`、`follow`、`unfollow` | 关注管理与动态 |
| `set_star` | 设置星标 |
| `delete_document` | 删除本人文档 |
| `write_report` | 写回报告并建索引 |

`get_article` 的正文位置使用 Unicode 字符偏移，`offset` 非负，`length` 默认 20000、最多 100000，结果含 `next_offset`。读取全文后再引用资料，避免把检索片段当作完整上下文。

`write_report` 接受标题、Markdown 正文及可选的关联文章 ID。同一标题与正文可幂等重试。报告写入 `reports/`，不进行收录审核或模型处理，索引失败由现有后台任务重试。调用方应将自己的综合分析与收录的原始资料区分。

## 库目录与过渡收录

`GET /api/v1/libraries` 返回当前空间全部库，`GET /api/v1/libraries/:id` 返回单库及内容类型、关系类型。MCP `list_libraries` 无参数，返回 `libraries` 数组。每项包含 `id`、`directory`、`name`、`system_role`、`template`、`description`、`collect_content`、`exclude_content`、`review_criteria`、`source_evidence_rule`、`deduplication_rule`、`template_rules`、`content_types`、`relation_types`、`feed_enabled`。`collect_content` 取说明，`exclude_content` 取审核标准。去重说明和处理规则由模板统一生成。

系统库为机构发布（`institutional`）、人物观点（`opinions`）、参考文献（`references`）、我的资料（`mine`）、AI 报告（`ai-reports`）。角色、目录和模板创建后不可修改，资料也不能换库。HTTP 另返回 `display_name`，默认系统名称按请求语言本地化，用户改名后保留用户名称；`name` 始终是存储值。

`POST /api/v1/libraries` 必填 `template`、`directory`、`name`，可选 `description`。模板为 `external_timely`、`external_durable`、`own`、`generated`。目录只允许小写字母、数字和分隔用的连字符，在空间内唯一。自建库的内容类型和关系类型初始为空；外部时效型默认要求链接，其余来源证据规则由模板固定。

`PATCH /api/v1/libraries/:id` 或 `PUT` 修改 `name`、`description`、`review_criteria`、`source_evidence_rule`、`content_types`、`relation_types`、`feed_enabled`。两个类型数组整体替换，元素包含 `name`、`enabled`；内容类型另含 `extract_graph`，排序按数组顺序保存。只有外部时效型接受 `source_evidence_rule`，值为 `link_required` 或 `link_or_publisher_date`。提交 `template`、`directory`、`system_role` 返回校验错误。`DELETE /api/v1/libraries/:id` 仅接受没有资料记录的自建库。

本阶段收录契约仍使用原来源类别：文章进入机构发布，本人文档进入我的资料，`write_report` 进入 AI 报告；尚不接受指定库、`source_ref` 或 `identifier`。模板规则现阶段只提供配置与读取，后续再接入收录审核和去重。`GET /api/v1/entries` 和未指定库的搜索读取空间全部资料。旧 `/api/v1/library` 是机构发布设置的兼容入口，其中向量模型设置写入空间配置；所有库共用空间向量模型。
