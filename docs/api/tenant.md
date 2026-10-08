# 个人空间 API

[返回目录](./README.md)

系统只有一个个人空间，ID 固定为 `1`。登录用户、API Key 和后台任务都使用该空间。

| 方法 | 路径 | 描述 |
| --- | --- | --- |
| GET | `/tenants/1` | 获取空间信息 |
| PUT | `/tenants/1` | 更新空间设置 |
| GET | `/tenants/1/api-keys` | 列出 API Key |
| POST | `/tenants/1/api-keys` | 创建 API Key |
| PUT | `/tenants/1/api-keys/:key_id` | 更新 API Key |
| DELETE | `/tenants/1/api-keys/:key_id` | 撤销 API Key |

API Key 使用 `X-API-Key`，拥有与登录用户相同的完整权限。创建和更新参数为 `name` 与可选的 `expires_at_unix`（Unix 秒）。

空间 KV 配置继续使用 `/tenants/kv/:key`，租户由认证上下文提供。模型、RSS 数据源、工作流、MCP 客户端等接口保持业务行为，详细配置请参考 Swagger。
