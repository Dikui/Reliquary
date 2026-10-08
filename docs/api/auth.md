# 账号认证 API

[返回目录](./README.md)

第一个注册账号是唯一用户，属于个人空间 `1`。之后注册接口返回拒绝。

| 方法 | 路径 | 描述 |
| --- | --- | --- |
| GET | `/auth/config` | `registration_enabled` 与 `minimum_password_length` |
| POST | `/auth/register` | 创建首个账号 |
| POST | `/auth/login` | 账号密码登录 |
| POST | `/auth/refresh` | 刷新登录令牌 |
| POST | `/auth/logout` | 撤销登录会话 |
| GET | `/auth/me` | 当前账号与个人空间 |
| POST | `/auth/change-password` | 修改自己的密码 |

注册使用 `username`、`email`、`password`，登录使用 `email`、`password`。密码至少 8 个字符。登录响应包含 `user`、`active_tenant`、`token` 和 `refresh_token`。

私有接口使用 `Authorization: Bearer <token>` 或 `X-API-Key`。两种凭证具有相同的完整权限，API Key 对应唯一用户。忘记密码使用服务器上的 `go run ./cmd/server reset-password`，新密码从标准输入读取，详见[个人使用说明](../personal-use.md)。
