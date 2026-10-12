# RSS 数据源导入与开发

数据源仅保留 `internal/datasource/connector/rss`，支持 RSS、Atom、JSON Feed 和现有站点适配器。订阅源全局配置，内部绑定唯一知识库，继续使用数据源框架、差异同步和定时调度器。

在设置的订阅源页面添加 RSS，填写订阅 URL 或选择站点适配器，验证后保存。可选择全量或增量同步、同步周期。需要认证的订阅可以填写 `auth_headers`，凭据继续加密保存。

同步流程为调度器或手动触发任务，读取 RSS 源，识别新建和更新条目，再交给现有知识入库与解析流程。同步结果保存在 `sync_logs`。RSS 订阅自然淘汰旧条目，不据此删除已有知识。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/datasource/types` | 查询 RSS 及站点适配器配置 |
| POST | `/api/v1/datasource/validate-credentials` | 验证订阅配置 |
| POST | `/api/v1/datasource` | 创建数据源 |
| GET | `/api/v1/datasource?kb_id=xxx` | 列出数据源 |
| POST | `/api/v1/datasource/:id/sync` | 手动同步 |
| POST | `/api/v1/datasource/:id/pause` | 暂停 |
| POST | `/api/v1/datasource/:id/resume` | 恢复 |

站点适配器位于 RSS 包内，沿用已有 SSRF 防护、内容解析与游标规则。新增适配器时更新 RSS 元数据和相应测试，不引入外部平台连接器。
