# 账号认证 API

[返回目录](./README.md)

使用账号密码登录。首次部署仅开放首个账号注册，注册成功后该账号成为团队 owner 和系统管理员。之后使用团队邀请链接注册，所有账号属于团队 `1`。

| 方法 | 路径 | 描述 |
| --- | --- | --- |
| GET | `/auth/config` | 获取当前注册模式及密码策略 |
| POST | `/auth/register` | 注册首个账号 |
| POST | `/auth/invitations/lookup` | 使用 token 查询邀请 |
| POST | `/auth/register-by-invite` | 使用 token 注册加入团队 |
| POST | `/auth/login` | 账号密码登录 |
| POST | `/auth/refresh` | 刷新登录令牌 |
| POST | `/auth/logout` | 撤销登录会话 |
| GET | `/auth/me` | 当前账号、团队和成员角色 |
| POST | `/auth/change-password` | 修改密码 |

注册和登录请求使用 `username`、`email`、`password` 中对应的字段；邀请注册另需 `token`。密码为 8 至 32 个字符且包含字母与数字，可通过系统密码策略要求大小写与特殊字符。邀请的有效性、角色以及账号和成员创建在数据库事务中校验，失败时不留下半注册账号。

登录响应继续返回 `active_tenant`、`memberships`、`token` 和 `refresh_token`，团队 ID 始终为 `1`。访问私有接口使用 `Authorization: Bearer <token>`；程序集成可使用团队 API Key。
