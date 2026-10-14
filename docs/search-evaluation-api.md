# 检索记录与测评 HTTP API

所有路径在 `/api/v1` 下，使用现有 JWT 或 API Key 鉴权。响应为 `{"success":true,"data":...}`；错误沿用现有错误中间件。列表使用 `limit` 和 `offset`，默认 50，最大 200。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/search/records` | 检索记录查询 |
| GET、POST | `/evaluation/sets` | 测评集列表、新建 |
| GET、PUT、DELETE | `/evaluation/sets/:set_id` | 读取、完整更新、删除测评集 |
| GET、POST | `/evaluation/sets/:set_id/questions` | 题目列表、新建 |
| GET、PUT、DELETE | `/evaluation/sets/:set_id/questions/:question_id` | 读取、完整更新、删除题目 |
| POST | `/evaluation/sets/:set_id/from-record` | 从记录转题 |
| POST | `/evaluation/sets/:set_id/candidates` | 模型生成待确认候选 |
| POST | `/evaluation/sets/:set_id/questions/:question_id/confirm` | 人工确认 AI 候选 |
| POST | `/evaluation/sets/:set_id/runs` | 同步运行评测 |
| GET | `/evaluation/runs` | 运行列表，可按 `set_id` 筛选 |
| GET | `/evaluation/runs/:run_id` | 配置快照、逐题结果、汇总 |
| GET | `/evaluation/vector-sets` | 当前及可用候选、归档向量集，返回 selector、model_id、dimension、profile_hash、run_id |
| GET | `/evaluation/compare?before=:id&after=:id` | 两次运行按题与汇总比较 |

测评集写入 `name` 和 `description`。题目写入示例：

```json
{
  "method": "ordinary",
  "request": {
    "query": "校准步骤是什么？",
    "libraries": ["mine"],
    "collapse_similar": false
  },
  "answers": [{"entry_id": "document-id", "grade": 1}]
}
```

`request` 使用普通检索的参数，可包含类型、实体、主题、时间与星标筛选。库目录在保存时解析为稳定 ID。`grade` 省略时为 1，范围为 0 至 10；0 表示不相关。答案必须属于所选库，不允许重复 ID。`answers: []` 允许构造无答案题。`method` 为 `ordinary` 或 `graph`。图谱题可在 `request.graph_parameters` 保存原图谱请求 JSON，图谱记录转题时自动保留原请求，供适配器读取实体、跳数等参数。普通检索拒绝该字段。图谱题运行通过 `EvaluationService.GraphSearch` 调用实际 `GraphSearchService.Search`，以题目外层已校验的 `request.libraries` 覆盖 graph_parameters 内的库，以运行 K 覆盖 limit。资料级 documents 列表参与指标计算；图谱查询不调用模型，逐题 snapshot 保存有效请求、扩展预算、截断与未同步计数。实体候选不明确时题目失败，需要人工改为选定 ID；Neo4j 不可用时保存明确错误。图谱检索同样记录 evaluation 来源与运行 ID。完整字段见 [图谱检索接口](search-api.md#图谱检索)。

转题请求为 `{"record_id":"...","answers":[{"entry_id":"..."}]}`。题目保存当次 `suggested_results`，供界面勾选；再次 PUT 可以修改答案。题目的来源由服务决定，不能由客户端伪造。AI 题的来源为 `ai`，状态为 `pending`，普通 PUT 不能绕过人工确认。

候选生成请求为 `{"libraries":["mine"],"entry_ids":["..."],"count":1,"model_id":"..."}`。最多 20 题、50 篇资料、60000 个正文字符，超出时明确拒绝；不截断正文。模型省略时使用库的 generation 模型。模型输出限定在所给资料范围，完整批次验证后才保存。确认请求为 `{}`，使用已有候选答案；也可传 `answers` 明确替换。重复确认返回 400。

运行请求示例：

```json
{
  "k": 5,
  "vector_set": "active",
  "collapse_similar": false,
  "retrieval_config": {"rrf_vector_weight": 0.7, "rrf_keyword_weight": 0.3},
  "rerank_model_id": ""
}
```

`k` 默认 10，范围 1 至 100。`vector_set` 支持 `active`、`candidate:<embedding_rebuild_run_id>`、`archive:<switched_run_id>`。候选必须完成、计数完整且未切换；归档必须仍然存在，且向量有唯一明确的模型和维度。它们共用普通检索的召回、融合、重排和相似折叠流程，不切换空间设置。归档模型已删除或候选模型语义配置已变化时拒绝运行。

省略 `retrieval_config` 时冻结运行开始时的空间配置；传入时按完整配置替换，未设置的项使用检索服务默认值。`rerank_model_id` 省略时使用库设置，空字符串显式关闭。每题保存真实执行参数、有效默认值、向量集及模型 profile hash、重排模型 ID、分块与索引配置、代码 revision、折叠规则。运行保存题目、答案与版本的快照。活动向量模型或 profile 在运行期间改变会导致运行失败。删除测评集或题目不会删除历史运行。

运行状态为 `running`、`completed` 或 `failed`。请求取消和检索失败会保存失败状态及逐题错误；汇总按全部题目求平均，失败题计零，失败运行不能用于比较。同步执行不提供队列续跑；进程异常退出时可能保留 `running` 状态，可查看已有结果后重新运行。

答案资料软删或物理删除时，数据库触发器将题目的 `answer_deleted` 设为 true。原答案 ID 和等级保留，题目仍可读取，运行前拒绝该题集，需人工更新题目。

比较限于同一测评集的两个完成运行。`delta` 为 after 减 before，逐题返回两侧结果、指标差值、`added_hits` 与 `lost_hits`。题目改变时有 `question_changed`，新增或移除题目通过 `change` 标明，不按列表位置错误配对。

检索记录筛选支持 `source=web|http|mcp|evaluation`、`method=ordinary|graph`、`library`（稳定 ID）、`query`、`run_id`、`from`、`to`（RFC3339）、`include_evaluation=true`。默认排除评测检索；显式 source=evaluation、run_id 或 include_evaluation 可读取。记录保留期限由 `RELIQUARY_SEARCH_RECORD_RETENTION_DAYS` 设置，默认 180，每小时清理，维护暂停时跳过清理。

五项指标均先对主结果资料 ID 去重并截取前 K 项，折叠成员不计命中。Precision@K 的分母固定为 K；Recall@K 的分母为所有正相关答案数；MRR 为首个命中排名的倒数；MAP 为各命中位置 Precision 之和除以全部正相关答案数；NDCG@K 使用 `(2^grade-1)/log2(rank+1)`，理想排序由全部答案按等级排序后取 K 项。无正相关答案时五项均为零。汇总为所有题目的算术平均。

测评集、题目、标准答案、运行及逐题结果通过 `PersistentTables` 进入 `.reliquary/data/`。检索记录通过 `BackupOnlyTables` 分类，只随 PostgreSQL 完整备份保存。


图谱题请求示例：

```json
{
  "method":"graph",
  "request": {
    "query":"Atlas Lab 的合作机构",
    "libraries":["mine"],
    "graph_parameters": {
      "entity":{"id":"atlas-entity-id"},
      "mode":"neighbors",
      "hops":2,
      "relation_types":["合作"]
    }
  },
  "answers":[{"entry_id":"source-document-id","grade":1}]
}
```

query 是题目文本；实体、目标和查询模式由 graph_parameters 提供。嵌入请求中的 libraries 不能扩大题目范围。普通检索的向量集和重排选项不会改变图谱查询。

## 外部接入与运行检查

测评管理与运行使用本文 HTTP 接口，MCP 的 `search` 与 `graph_search` 供正常检索；没有独立的测评 MCP 工具。创建题目前先用 [库清单](external-integration.md#选择库) 取得范围，答案资料必须在该范围内。运行接口同步返回完成或失败状态，客户端应为实际检索耗时配置足够的 HTTP 超时。评估报告须保留 K、向量集、重排选择、折叠选择和题目修订，替身服务验收只能证明执行流程与指标计算。
