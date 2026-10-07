# 团队空间 API

[返回目录](./README.md)

系统只有一个团队空间，ID 固定为 `1`。所有登录、邀请、API Key、飞书集成和后台任务都使用该团队。

| 方法 | 路径 | 描述 |
| --- | --- | --- |
| GET | `/tenants/1` | 获取团队信息 |
| PUT | `/tenants/1` | 更新团队设置 |
| GET | `/tenants/1/api-keys` | 列出团队 API Key |
| POST | `/tenants/1/api-keys` | 创建团队 API Key |
| PUT | `/tenants/1/api-keys/:key_id` | 更新团队 API Key |
| DELETE | `/tenants/1/api-keys/:key_id` | 撤销团队 API Key |
| GET | `/tenants/1/members` | 查看团队成员 |
| PUT | `/tenants/1/members/:user_id` | 调整成员角色 |
| DELETE | `/tenants/1/members/:user_id` | 移除成员 |
| GET | `/tenants/1/invitations` | 查看团队邀请 |
| POST | `/tenants/1/invite-links` | 创建邀请注册链接 |

团队 API Key 使用 `X-API-Key`，无需指定租户。保留检索、入库、完整权限范围，以及知识库范围限制。owner / admin 管理团队；contributor 可以编辑全部知识库，viewer 只能读取。

团队 KV 配置继续使用 `/tenants/kv/:key`，租户由认证上下文提供。模型、数据源、工作流、Wiki、Skills、MCP 等接口保持原有业务行为，详细配置请参考 Swagger。
