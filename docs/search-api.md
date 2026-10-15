# 普通检索接口

HTTP 为 `POST /api/v1/search`，网页不带凭据，外部工具使用收录 API Key。MCP 为 `search`，参数相同。HTTP 返回 `{"success":true,"data":...}`；MCP 使用同样的资料字段，资料 ID 为 `entry_id`。

```json
{
  "query": "仪器校准",
  "libraries": ["mine", "references"],
  "content_types": ["教程"],
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
| `entry_id` | HTTP 与 MCP 的资料 ID |
| `library_id`, `library_name`, `library_directory` | 所属库 |
| `title`, `summary`, `entities`, `topics`, `starred` | 资料信息 |
| `source_unverified`, `incomplete` | 来源未核实、待补全 |
| `has_update`, `latest_entry_id` | 同一来源标识是否有新记录及最新记录 ID |
| `score`, `similar_reports` | 最终排序分数及同库相似报道 |
| `snippets` | 最多三个命中片段 |

同一 source_ref 的旧记录返回 has_update=true 时，网页资料列表、卡片、详情和检索结果显示“有更新的记录”，点击可打开 latest_entry_id 指向的新资料。字段缺失或 has_update=false 时不显示更新标记；旧资料仍可读取与检索。

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

网页引用先核对正文哈希，再转换 UTF-16；外部调用方可使用同样的方法：

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

搜索页和全局搜索框提供库选择并记忆所选范围。全部库范围根据 `GET /api/v1/libraries` 动态解析，显式选择时传所选 ID。库列表读取失败时不发送无库检索；结果显示 `library_name`。HTTP 与 MCP 均严格要求库参数。

## 资料返回中的库名

资料详情、列表、批量读取、待审列表、待补全列表、实体关联资料、检索、关注动态及 MCP 返回中的 `library_name` 均使用所属库当前保存的 `libraries.name`，与 `library_id` 对应。改名后下次读取立即生效，库名不写入资料表。MCP `get_entry` 在顶层返回 `library_id`、`library_name`，同时保留原有 `header.library_id` 和 `header.library`。

`library_name` 不随请求语言翻译，与检索结果保持一致。库列表接口另外提供 `display_name`：未改名的系统库按请求语言本地化，已改名的系统库和自定义库保留所保存的名称。需要本地化的客户端可以通过 `library_id` 对应库列表的 `display_name`。

## 图谱检索

HTTP 为 `POST /api/v1/graph/search`，MCP 工具为 `graph_search`。两者复用 `GraphSearchService.Search`，图谱响应 JSON 字段完全一致。HTTP 包装为 `{"success":true,"data":...}`，MCP 在 `structuredContent` 与文本内容中返回同一 data。网页不带凭据，外部工具使用 API Key 鉴权，MCP 使用 Bearer API Key。检索过程不调用模型。

```json
{
  "libraries": ["opinions", "institutional"],
  "entity": {"name": "Atlas Lab"},
  "mode": "neighbors",
  "hops": 2,
  "relation_types": ["合作"],
  "limit": 20
}
```

| 请求字段 | 含义 |
| --- | --- |
| `libraries` | 必填，1 至 100 个当前空间的库 ID 或英文目录，返回和记录解析后的稳定 ID |
| `entity` | 必填，`{"id":"实体 ID"}` 或 `{"name":"规范名或别名"}`，二选一 |
| `target` | path 模式必填，格式同 entity；neighbors 模式不能指定 |
| `mode` | neighbors 或 path，默认 neighbors |
| `hops` | 1 至 3，默认 1；path 模式表示路径最大长度 |
| `relation_types` | 可选，最多 100 个关系类型名称，空数组表示全部 |
| `limit` | 1 至 100，默认 20；限制邻居边数或路径数，并独立限制资料级 documents 列表 |

名称按实体档案既有 NFKC、大小写和空白规范化规则精确匹配规范名与别名，不使用模型或模糊猜测。ID 查询只接受未合并的现有实体；已合并 ID 返回 404，调用方可通过实体详情取得保留档案 ID。无匹配返回 404。多个匹配返回 200、`status="needs_selection"`，在 `candidates.entity` 或 `candidates.target` 中列出 ID、规范名、实体类型与别名，边和路径为空。调用方选定 ID 后重新请求，不自动选第一个候选。无效参数或未知库返回 400。

路径示例：

```json
{
  "libraries": ["mine"],
  "entity": {"id": "atlas-entity-id"},
  "target": {"id": "gamma-entity-id"},
  "mode": "path",
  "hops": 3,
  "limit": 10
}
```

两种模式都沿入边和出边扩展；`subject_id` 与 `object_id` 始终保留正本方向。路径是无重复实体的简单路径，返回遍历方向上的 `entity_ids` 和对应 `relation_ids`，不保证返回全部路径。只沿所选库的边扩展，关系类型在 Neo4j 的 LIMIT 之前筛选。邻居按跳数、实体 ID 与关系 ID 确定顺序；路径在有界子图中确定遍历顺序。没有语义重排，资料级分数固定为 1。

响应示例，省略资料级 documents 的其他普通检索字段：

```json
{
  "status": "ok",
  "libraries": ["mine-library-id"],
  "mode": "neighbors",
  "hops": 1,
  "expansion_limit": 100,
  "candidates": {},
  "entities": [
    {"id":"atlas-entity-id","canonical_name":"Atlas Lab","entity_type":"organization","aliases":[]},
    {"id":"beta-entity-id","canonical_name":"Beta Group","entity_type":"organization","aliases":[]}
  ],
  "edges": [{
    "id": "relation-id",
    "subject_id": "atlas-entity-id",
    "object_id": "beta-entity-id",
    "library_id": "mine-library-id",
    "relation_type": "合作",
    "version": 1,
    "source_count": 1,
    "sources": [{
      "entry_id": "source-document-id",
      "title": "合作资料",
      "library_id": "mine-library-id",
      "library_name": "我的资料",
      "library_directory": "mine",
      "published_at": "2026-10-01T00:00:00Z",
      "source_unverified": false,
      "acquisition": "verbatim",
      "evidence": [{
        "locations": [{"heading_path":["合作"],"paragraph":2,"start":10,"end":20,"body_hash":"hash","mapping":"exact"}],
        "speaker": {"entity_id":"speaker-id","name":"发言人","selection":"author"}
      }]
    }]
  }],
  "paths": [],
  "documents": [{"entry_id":"source-document-id","title":"合作资料","library_id":"mine-library-id","score":1}],
  "truncated": false,
  "truncations": [],
  "pending_sync": {"global":0,"involved":0}
}
```

`RELIQUARY_GRAPH_EXPANSION_LIMIT` 设置每跳所有节点共用的扩展预算，默认 100，允许 1 至 1000。每个节点最多读取剩余预算加一条，额外一条仅用于检测截断，不继续扩展。入边、重复边和之后被 PG 过滤的旧边也消耗预算，因此副本落后可能减少可见结果。未读完的节点或该层后续节点会写 `truncations`，reason 为 `expansion_limit`，包含 hop 和可用时的 entity_id。最终邻居边超出 limit 标为 `result_limit`，路径超出 limit 标为 `path_limit`，路径枚举超过固定 10000 个状态标为 `path_work_limit`，资料级列表超限标为 `document_limit`。存在任一条时 `truncated=true`。截断结果不能用来断言实体间没有路径。

每批 Neo4j 边从 PG 回填并校验关系 ID、方向、库、关系类型和两端实体存在性，PG 不再承认的旧边不会继续扩展。来源资料必须在当前空间、所选库和边所属库中存在且未删除。没有有效来源的边不返回，也不能作为路径桥梁。sources 的 evidence 保留位置和人物观点发言人，published_at 缺失时为 null，不用收录时间替代。source_count 为本次可见来源数，version 为 PG 正本版本。documents 为按最终边顺序出现的资料去重列表，位置映射为 snippets.locations，供检索记录与测评使用。

`pending_sync.global` 是当前个人空间内 `completed_at IS NULL` 的记录数，不按库缩小。`involved` 是涉及最终返回实体和查询端点的记录数；候选响应使用全部候选实体。统计按记录行计数，同一实体多条记录分别计数，两项在同一个 PG 查询中读取。计数提示副本可能落后，不能证明 PG 和 Neo4j 是同一时间点快照。多次 Neo4j 读取与 PG 回填之间不持有跨数据库锁。Neo4j 连接或查询失败时，HTTP 返回 503 和明确的 `Neo4j query replica unavailable` 错误，MCP 返回同样含义的工具错误。

服务入口统一保存 `Method="graph"` 的检索记录：原始请求、实体名或 ID、已校验的稳定库 ID、documents 排名与耗时。HTTP、网页和 MCP 来源沿用可信上下文，测评来源携带运行 ID。有效范围内的查询失败也保存错误；保存记录失败会返回错误。
