# 模型调用统计 API

调用账本记录每次实际发出的 chat、视觉 chat、embedding、图片向量和 rerank HTTP 请求。SDK、原始 HTTP、Ollama 和 ChatGPT 登录生成均在相同 transport 边界记账。排队、限流、模型下载、凭据刷新和未发出请求的参数校验不计入调用。

模型创建与更新 API 接受 `parameters.pricing`：

```json
{"input_per_million": 2, "output_per_million": 4, "currency": "USD"}
```

单位为每百万 token 的币种金额。单价可为 null，币种缺省为 USD，提供时要求三个大写字母。更新省略 pricing 会保留现有价格，显式 null 清空价格。模型响应返回 pricing。每次请求保存单价、币种和模型名称快照，历史记录不会随配置变更。

网页在“设置 > 模型”的新建和编辑表单提供输入单价、输出单价和币种。两项单价可分别留空，明确的 0 表示免费；币种留空时使用 USD。三项全部清空会提交 pricing=null。保存新价格只影响之后发出的调用，历史快照保持原值。ChatGPT 订阅模型显示费用不适用说明，不提供 token 单价填写。

费用状态为 `calculated`、`unknown` 或 `not_applicable`。`calculated` 是按模型配置计算的金额，不代表提供方账单。ChatGPT 登录账号始终为 `not_applicable`，cost 为 null。无 usage 的 token 字段为 null；明确返回的 0 保持为 0。输入型 embedding 和 rerank 接受 total_tokens 作为输入 token，缺失的输出 token 保持 null。

所有接口位于 `/api/v1`，沿用登录和 API Key 认证：

| 方法与路径 | 功能 |
| --- | --- |
| GET /model-calls | 调用明细 |
| GET /entries/:id/model-calls | 资料的全部调用明细，通过分页读取 |
| GET /model-calls/summary | 按模型、用途和日、周、月汇总 |
| GET /model-calls/trend | 按日汇总趋势 |
| GET /model-calls/purposes | 集中定义的用途枚举 |

通用筛选参数为 `from`、`to`（RFC3339，左闭右开）、`model_id`、`purpose`、`entry_id`。资料路径的 ID 优先于 entry_id 查询参数。明细支持 `limit`（默认 100，上限 1000）与 `offset`，返回 total；按 requested_at、id 倒序。

summary 支持 `period=day|week|month`（默认 day）、`group_by=model,purpose`（默认两者，可单选）。时间桶采用 UTC，周从周一开始；trend 固定 day，只按日期和币种分组，没有调用的日期不返回桶。每个币种独立汇总，禁止不同币种相加。

汇总包含 calls、failures、failure_rate（0 到 1）、average_duration_ms、input_tokens、output_tokens、cost，以及 unknown_input_calls、unknown_output_calls、unknown_cost_calls、not_applicable_cost_calls。SUM 仅加已知值，全未知时返回 null；只要未知调用数大于零，金额或 token 就是部分总和。成功率与耗时包含所有请求。

明细的 attempt 从 1 开始，表示一次模型方法调用中实际发出的第几个请求，包括 HTTP 重试和图片兼容回退。retry_number 从 0 开始，表示外围任务执行的重试次数。调用方可用 `types.WithModelCallRetry` 标注自身重试，用 `types.WithModelCallEntry` 标注资料，用 `types.WithLLMCallMetadata` 标注用途。测评任务使用 evaluation_generation，未映射的阶段归到 other。

写入采用同步 best effort，独立于业务取消，最多等待一秒，错误只写日志。这样避免内存队列在退出时丢失统计，代价是每个请求增加一次 PostgreSQL INSERT。流取消时还有独立回调，即使生产者没有继续读取或关闭响应也会完成中断记账。耗时在写入前取值，不包含记账耗时；不会保存正文、响应内容或凭据。进程被强制终止时尚未完成的请求没有持久记录。

`RELIQUARY_MODEL_CALL_RETENTION_DAYS` 缺省或 0 表示不清理；可设置为 730 等正整数。现有 housekeeping 在未暂停维护时清理过期记录，关闭 housekeeping 时不自动清理。账本没有模型或资料外键，因此删除资料或模型不会删除历史；统计表登记为 BackupOnlyTables，仅随 PostgreSQL 完整备份，不进入维护副本。SQL 备份核对保留行数和摘要，重建或重配时允许追加新调用，已有行的摘要仍须一致。

## 接入与维护边界

外部工具用 API Key 调用本文 HTTP 统计接口，没有单独的统计 MCP 工具。AI 报告不调用审核或处理模型，但向量索引调用仍记账。图谱检索不调用模型。普通检索可能调用查询向量和重排；不可用时的退回状态见 [检索接口](search-api.md)。

完整 PG 备份保存调用账本，仅文件库恢复不能恢复历史调用统计或检索记录。恢复后重建索引产生的调用追加到账本，不修改历史价格快照。替身返回的 token 与费用用于验证记账规则，不代表真实模型费用或效果。
