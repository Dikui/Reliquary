# 个人单用户使用

系统只有一个用户和一个个人空间，租户 ID 固定为 `1`。首个用户注册成功后，注册接口拒绝后续请求，登录页隐藏注册入口。

登录用户拥有全部权限，可以管理系统设置、运行队列、模型和领域模板。API Key 通过 `X-API-Key` 鉴权，具有相同的访问权限，可按名称和有效期管理、撤销。知识查询中的知识库、领域和时间条件继续作为业务筛选条件。

用户可在登录后修改自己的密码。忘记密码时，在仓库根目录导出与服务器相同的 `DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME` 环境变量，将新密码从标准输入交给本地命令：

```bash
go run ./cmd/server reset-password
```

输入新密码后按回车。命令直接更新唯一用户的密码哈希，并撤销旧登录令牌；密码最少 8 个字符，UTF-8 编码不超过 72 字节。本地重置没有 HTTP 接口。

数据源仅支持 RSS、Atom、JSON Feed 和 RSS 连接器内的站点适配器，保留定时同步、手动同步、订阅筛选及可选鉴权头。

决策依据见 [ADR 0007](adr/0007-personal-single-user.md)。

检索直接查询同一个 PostgreSQL（ParadeDB）数据库，保留 pgvector 与 BM25 混合检索、融合和重排。知识库可以选择嵌入模型，文件统一保存到 `LOCAL_STORAGE_BASE_DIR` 指定的本地目录。备份需包含 PostgreSQL 数据和该目录。

文件访问继续使用带鉴权的 `/files` 代理与 `/files/presigned`。配置 `APP_EXTERNAL_URL` 并代理 `/r/` 后，可生成限时匿名文件链接。

解析保留 `simple`、`anydoc`、`mineru` 和 `mineru_cloud`。文本、Markdown、图片和音频使用 simple；PDF、Office 和 EPUB 默认使用进程内 anydoc，CSV 默认使用 simple，也可显式选择 anydoc；扫描件和复杂排版可以在解析引擎设置中选择已配置的 MinerU。HTML、HTM、MHTML、XMind 文件上传返回明确的不支持错误，按网址收录继续使用现有抓取能力。

本地开发需先安装 Rust 工具链，然后在仓库根目录执行：

```bash
make anydoc-lib
go run -tags anydoc ./cmd/server
```

应用镜像默认以 `WITH_ANYDOC=1` 构建。普通 `go run ./cmd/server` 不链接 anydoc，PDF 和 Office 解析会提示静态库和 `-tags anydoc` 的构建要求；可以在设置中为 PDF 显式选择已配置的 MinerU。单次解析调用超时由 `PARSER_CALL_TIMEOUT` 设置，默认 30 分钟。

只运行一个后端进程。流式事件、模型并发与速率限制、接口限流、工具审批和 MCP OAuth state 保存在进程内存中，重启后不保留未完成的流式事件或审批；OAuth state 为单次使用，10 分钟过期。Redis 继续用于 Asynq 任务队列和知识处理锁。本轮暂留联网搜索、Wiki、技能和沙箱原有 Redis 用法，随对应功能的后续任务整体删除。运行配置的缓存变更通知也暂留 Redis 实现。
