# 字段参考

字段以当前 HTTP 与 MCP 返回为准；未返回某个标记不等于已核实。先读 [使用规则](../rules.md)，接口差异见 [HTTP 参考](http.md)。

## 位置与引用

普通检索的位置在 `results[].snippets[].locations[]`；图谱在 `edges[].sources[].evidence[].locations[]`，来源资料 ID 在 `sources[].entry_id`；自检在 `results[].findings[].evidence[].location`，每条证据自带 `entry_id`、`quote` 和可选 `current_result`。

| 字段 | 含义与用法 |
|---|---|
| `heading_path` | 从上到下的标题数组，例如 `["背景","方法"]`；引用时写成“背景 / 方法”，无标题时可能为空 |
| `paragraph` | 已保存 Markdown 的段落序号，从 1 开始；标题、列表等分块边界也参与编号，不凭渲染后的自然段自行重编号 |
| `start`、`end` | 原始已保存 Markdown 中从 0 开始的 Unicode 码点半开区间 `[start,end)`，包括 Markdown 标记；不是 UTF-8 字节或 UTF-16 单元 |
| `body_hash` | 完整已保存正文的 SHA-256 十六进制哈希，标明位置所对应的正文；不是片段哈希。正文改动后旧位置可能失效，应重新检索定位 |
| `mapping` | `exact` 为原文精确位置；`normalized_line_endings` 表示已将分块时规范化的换行映回保存正文，偏移仍针对保存正文。无位置时不可编造精确引用 |

一条命中可能跨多个标题或段落，因此有多个位置。先以资料 ID 调 `get_entry`，传 `offset=start`、覆盖到 `end` 的 `length` 并按需扩展上下文，再核对证据。引用至少包含资料 ID、标题路径、段落序号；不要把摘要或图片说明的生成文字当作原文，缺位置或证据过时时明确说明。

## 四种信任状态

| 标记 | agent 应如何处理 |
|---|---|
| `cannot_be_fact=true` | AI产出，不能作为事实依据；单列其观点与推论，追溯引用的原文。`acquisition=model_written` 是获取方式声明，写回结论必须设置；未带 `cannot_be_fact` 的投影仍需查看 `acquisition` |
| `source_unverified=true` | 保留的历史或显式人工来源未核实标记，在每处引用旁注明；新资料缺少链接不自动设置该标记。读取成功或处理完成不代表来源核实 |
| `has_update=true`、`latest_entry_id` | 已有最新资料，改读所指 ID 并重新定位证据，不沿用旧位置；最新资料不可读时说明缺口 |
| 图谱 `truncated=true` 或 `pending_sync` 有未同步记录 | 关系结果不完整，不能据此断言没有关系；输出查询范围与限制 |

普通检索、正文和关注动态会返回相应资料标记；图谱 `documents` 使用普通检索资料结构，但 `edges[].sources[]` 只含来源、`acquisition`、`source_unverified` 和位置等证据字段，不保证有全部资料标记，应再读原文核对。

## 检索与图谱状态

| 字段 | 含义与用法 |
|---|---|
| `vector_unavailable` | true 表示本次向量检索不可用，结果依赖可用的检索通道，向用户说明检索降级；false 不代表重排成功 |
| `rerank_status` | `applied` 表示已应用重排；`unconfigured` 表示未配置，`unavailable` 表示不可用，`no_candidates` 表示没有候选可重排。不是 `applied` 时说明本次排序未应用重排，不将分数解释为事实置信度 |
| `excluded_missing_published_at` | 按发布时间筛选时因缺少 `published_at` 被排除的资料数量；说明覆盖限制，不用收录时间补发布时间 |
| 图谱 `status`、`candidates` | `needs_selection` 表示名称有歧义，按 `entity` 或 `target` 对应的候选核对库和身份，再用 ID 查询；不能随意选第一个 |
| `truncated`、`truncations` | 是否截断，以及截断发生的跳数、实体和原因；未返回的边或路径不能解释为不存在 |
| `pending_sync.global`、`pending_sync.involved` | 分别为全空间与本次涉及范围尚未同步的记录数；不是布尔值，有记录时说明正本与图谱副本可能尚未一致 |

## 分页与相似报道折叠

`get_entry` 的 `offset` 默认为 0，`length` 默认为 20000，最大 100000，均按 Unicode 码点计数。返回 `body` 为本段，`length` 是实际返回码点数，`total_length` 是完整正文长度。存在 `next_offset` 时将它作为下次 `offset`，直到该字段省略；不把单段正文当成全文。故障材料文本读取也使用 Unicode 分段和 `next_offset`，材料图片则返回认证读取地址。

关注动态用 `next_cursor` 续页，将其原样作为 `cursor` 并保持查询范围，字段省略表示窗口已读完。`since` 按进入动态时间筛选，省略时用各库网页上次查看时间；MCP 和 HTTP 的读取调用均不更新该时间。`list_content_checks` 用 `page`，每页固定 20 条，返回 `items`、`total`；`list_pending` 用 `page`、`page_size`；`list_entities` 用 `offset`、`limit`，它们不能与正文的 Unicode 偏移混用。

`collapse_similar` 控制检索及关注动态的相似报道折叠；只在同库内按摘要向量相似度和发布时间窗口折叠，不持久保存分组，也不调用模型。每组主资料下的 `similar_reports` 保留其他资料各自的 ID、来源、标记和命中位置。需要独立证据时逐项读取，不把组数当成资料总数或把相似报道数当作独立来源数。关闭折叠可查看单独结果，但不会改变资料或来源的可信度。
