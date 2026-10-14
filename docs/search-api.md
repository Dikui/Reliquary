# 普通检索接口

HTTP 为 `POST /api/v1/search`，使用登录令牌或收录 API Key。MCP 为 `search`，参数相同。HTTP 返回 `{"success":true,"data":...}`；MCP 保留现有命名适配，资料 ID 字段为 `article_id`。

```json
{
  "query": "仪器校准",
  "libraries": ["mine", "references"],
  "content_types": ["tutorial"],
  "entity_ids": [],
  "topic_ids": [],
  "published_from": "2026-01-01",
  "starred": true,
  "collapse_similar": true,
  "limit": 20
}
```

`libraries` 必填，1 至 100 个库 ID 或稳定目录名。未知库、空数组和空字符串返回 400。每个库必须属于当前空间。多个库取并集；其他筛选之间取交集，同类筛选数组内取并集。保留 `sources` 兼容字段。`published_to` 支持 RFC3339 或日期；日期包含当天。缺少发布时间的命中排除，并计入 `excluded_missing_published_at`。所有候选通道在排序和截断之前应用库及其他筛选。

结果按资料合并，`results` 的主要字段为：

| 字段 | 含义 |
| --- | --- |
| `entry_id` | HTTP 资料 ID，MCP 沿用 `article_id` |
| `library_id`, `library_name`, `library_directory` | 所属库 |
| `title`, `summary`, `entities`, `topics`, `starred` | 资料信息 |
| `source_unverified`, `incomplete` | 来源未核实、待补全 |
| `has_update`, `latest_article_id` | 同一来源标识是否有新记录及最新记录 ID |
| `score`, `similar_reports` | 最终排序分数及同库相似报道 |
| `snippets` | 最多三个命中片段 |

`snippets` 保留 `chunk_id`, `content`, `chunk_index`, `score`, `match_type`, `image_path`。新增 `locations` 数组；跨段落或标题的分块可能具有多个位置。`mapping=exact` 的文字命中，位置切片按顺序拼接等于分块 `content`。既有分块器把 CRLF 规范为 LF 时，位置仍按原保存正文计数，标为 `normalized_line_endings`；先把切片中的 CRLF 替换成 LF，再与分块文字比较。图片向量命中的 `content` 可以为空，位置范围对应正文图片目标 URL，切片等于 `image_path`，不包含图片说明或 Markdown 括号。纯表头、摘要等生成文本没有正文对应范围时返回空数组，不伪造正文坐标。

```json
{
  "heading_path": ["实验", "校准"],
  "paragraph": 3,
  "start": 21,
  "end": 47,
  "body_hash": "保存后的正文 UTF-8 SHA-256",
  "mapping": "exact"
}
```

坐标仅覆盖保存后的 Markdown 正文，不包含文件表头。字符范围为 Unicode 码点的零基半开区间 `[start,end)`，不是 UTF-8 字节数或 JavaScript 字符串索引。标题路径包含祖先和当前位置的各级标题文字，不包含 Markdown 标记。段落序号从 1 开始；标题、每个列表项、连续表格及整段围栏代码各为一个块，普通段落以空行分隔，块后的空白归入前一块。代码内部的空行和 `#` 不产生标题或新段落。图片引用保持原始 Markdown 文本和范围，同时支持 Markdown 内行内 `<img>` 的目标 URL。正文哈希用于拒绝对不同正文应用旧坐标。

前端 ikb-18 应先核对正文哈希，再转换 UTF-16：

```js
const points = Array.from(body)
const utf16Start = points.slice(0, location.start).join('').length
const utf16End = points.slice(0, location.end).join('').length
const exactText = body.slice(utf16Start, utf16End)
```

例如 `A😀中` 中 emoji 占 1 个码点、2 个 UTF-16 code unit。图片目标补全或保存为附件相对路径后立即重算位置和哈希；重新分块仍使用现有算法。历史分块在重处理时生成位置。

响应级 `vector_unavailable=true` 表示查询向量不可用，本次使用全文检索。`rerank_status` 为 `unconfigured`、`unavailable`、`applied` 或 `no_candidates`。重排服务失败时保留融合排序；不会把失败当成无命中。

## 向量重建及切换

`GET /api/v1/embeddings/rebuild` 返回最近一次运行及失败项。已有重建控制动作继续可用。`POST` 同一路径：

```json
{"action":"start","model_id":"新向量模型 ID"}
```

提供 `model_id` 时创建独立候选向量集，不修改空间当前模型。返回运行含 `candidate=true`、`total_count`、`completed_count`、`failed_count`、`status`、`switched_at`。后台按分块、资料摘要、实体及已开启的图片向量保存检查点。模型不可用时保留待计算项，待处理操作延后一分钟且不计普通失败。服务恢复后继续计算。

```json
{"action":"switch","run_id":"运行 ID"}
{"action":"delete_old","run_id":"运行 ID"}
```

切换必须全部完成，并再次核对模型身份、源内容哈希及分块覆盖。计算中新增、修改或删除资料导致核对失败时应重新建立候选集。未完成的候选可用 `{"action":"cancel","run_id":"运行 ID"}` 取消，随后重新 start；旧任务会被隔离，不能继续写入。切换在一个数据库事务中完成，检索继续旧向量直到事务提交。旧向量独立保存，随后可删除；删除旧向量不删除当前向量或源资料。无 `model_id` 的 `start` 保留原有当前模型维护重建行为。

空间 `PUT /api/v1/space/kv/retrieval-config` 中 `image_vector_enabled` 默认 false。只有显式开启且模型支持图片时计算、检索图片向量；开关不影响 Markdown 图片引用或文字检索。


### 网页默认库范围

在网页加入库选择控件前，搜索页和全局搜索框共用的请求适配器先读取 `GET /api/v1/libraries`，再将当前全部库 ID 放入 `libraries`。调用方显式传入库时保持原值。库列表读取失败时不发送无库检索；结果显示 `library_name`。HTTP 与 MCP 后端仍严格要求库参数。

## 资料返回中的库名

资料详情、列表、批量读取、待审列表、待补全列表、实体关联资料、检索、关注动态及 MCP 返回中的 `library_name` 均使用所属库当前保存的 `libraries.name`，与 `library_id` 对应。改名后下次读取立即生效，库名不写入资料表。MCP `get_article` 在顶层返回 `library_id`、`library_name`，同时保留原有 `header.library_id` 和 `header.library`。

`library_name` 不随请求语言翻译，与检索结果保持一致。库列表接口另外提供 `display_name`：未改名的系统库按请求语言本地化，已改名的系统库和自定义库保留所保存的名称。需要本地化的客户端可以通过 `library_id` 对应库列表的 `display_name`。
