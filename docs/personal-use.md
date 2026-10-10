# 个人单用户使用

系统只有一个用户和一个个人空间，租户 ID 固定为 `1`。首个用户注册成功后，注册接口拒绝后续请求，登录页隐藏注册入口。

登录用户拥有全部权限，可以管理系统设置、运行队列、模型和实体档案。API Key 通过 `X-API-Key` 鉴权，具有相同的访问权限，可按名称和有效期管理、撤销。知识查询中的知识库和时间条件继续作为业务筛选条件。

用户可在登录后修改自己的密码。忘记密码时，在仓库根目录导出与服务器相同的 `DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME` 环境变量，将新密码从标准输入交给本地命令：

```bash
go run ./cmd/server reset-password
```

输入新密码后按回车。命令直接更新唯一用户的密码哈希，并撤销旧登录令牌；密码最少 8 个字符，UTF-8 编码不超过 72 字节。本地重置没有 HTTP 接口。

数据源仅支持 RSS、Atom、JSON Feed 和 RSS 连接器内的站点适配器，保留定时同步、手动同步、订阅筛选及可选鉴权头。

决策依据见 [ADR 0007](adr/0007-personal-single-user.md)。

检索直接查询同一个 PostgreSQL（ParadeDB）数据库，保留 pgvector 与 BM25 混合检索、融合和重排。知识库可以选择嵌入模型，文件统一保存到 `LOCAL_STORAGE_BASE_DIR` 指定的本地目录。备份需包含 PostgreSQL 数据和该目录。

生成模型只使用三种接口：OpenAI 兼容、Anthropic 原生和 Ollama 本地。服务商下拉框从后端预设表读取，包含通用 OpenAI 兼容、OpenAI、Anthropic、DeepSeek、阿里云百炼、智谱、月之暗面、硅基流动、火山方舟、OpenRouter、Google Gemini 和 Ollama。其他服务可选择通用 OpenAI 兼容并填写地址。Gemini 使用官方兼容地址 `https://generativelanguage.googleapis.com/v1beta/openai/`。Ollama 模型列表和拉取管理继续保留。

嵌入使用 OpenAI 兼容接口或 Ollama。阿里云文本嵌入默认地址为 `https://dashscope.aliyuncs.com/compatible-mode/v1`，智谱和硅基流动嵌入也使用兼容接口。重排使用通用 `/rerank` 格式或阿里云百炼原生接口；硅基流动、Jina、vLLM 和 Xinference 等兼容服务可使用通用格式。服务商预设按模型类型筛选，具体模型需支持相应接口。模型测试调用、思考模式、并发与速率限制、模型分级和路由继续保留。

文件访问继续使用带鉴权的 `/files` 代理与 `/files/presigned`。配置 `APP_EXTERNAL_URL` 并代理 `/r/` 后，可生成限时匿名文件链接。

解析保留 `simple`、`anydoc`、`mineru` 和 `mineru_cloud`。文本、Markdown 和图片使用 simple；PDF、Office 和 EPUB 默认使用进程内 anydoc，CSV 默认使用 simple，也可显式选择 anydoc；扫描件和复杂排版可以在解析引擎设置中选择已配置的 MinerU。HTML、HTM、MHTML、XMind 和音频文件上传返回明确的不支持错误，按网址收录使用 HTTP 抓取；需要 JavaScript 渲染的页面返回无法抓取正文的失败原因。

本地开发需先安装 Rust 工具链，然后在仓库根目录执行：

```bash
make anydoc-lib
export SSRF_WHITELIST_EXTRA="${SSRF_WHITELIST_EXTRA-198.18.0.0/15}"
go run -tags anydoc ./cmd/server
```

应用镜像默认以 `WITH_ANYDOC=1` 构建。普通 `go run ./cmd/server` 不链接 anydoc，PDF 和 Office 解析会提示静态库和 `-tags anydoc` 的构建要求；可以在设置中为 PDF 显式选择已配置的 MinerU。单次解析调用超时由 `PARSER_CALL_TIMEOUT` 设置，默认 30 分钟。

只运行一个后端进程。模型并发与速率限制、接口限流保存在进程内存中，重启后重新计数。Redis 继续用于 Asynq 任务队列及任务检查、多模态图像任务计数、知识移动进度，以及运行配置缓存变更通知。

按网址收录、RSS 与站点适配器继续可用，使用带 SSRF 防护的 HTTP 抓取。综合分析和报告由外部 AI 工具完成。

Docker Compose、`make run`、`scripts/dev.sh app` 和委托该脚本的 `scripts/quick-dev.sh` 默认设置 `SSRF_WHITELIST_EXTRA=198.18.0.0/15`。这一基准测试保留网段正常公网不会使用；本机代理采用 fake-ip 时，公网域名可能解析到该网段，默认放行使按网址收录和抓取继续可用。该项不会放行回环、私网、链路本地地址、`localhost`、`postgres` 或 `redis` 等 Docker 服务名，已有 SSRF 校验规则继续生效。

Docker Compose 和开发脚本可在 `.env` 中设置自定义 `SSRF_WHITELIST_EXTRA` 覆盖默认值，多个值用逗号分隔；显式写入 `SSRF_WHITELIST_EXTRA=` 关闭默认放行，修改后需重启应用。Docker Compose 也可用 `SSRF_WHITELIST_EXTRA= docker compose up -d app` 关闭。宿主机开发脚本优先使用调用方已设置的环境变量，其次为 `.env.local`、`.env`，最后为默认值；`SSRF_WHITELIST_EXTRA= ./scripts/dev.sh app` 同样关闭。`docker-compose.dev.yml` 仅启动 PostgreSQL 和 Redis，宿主机后端通过上述脚本获得该默认值。直接 `go run` 或执行二进制时，请先按上面的 `export` 示例设置此变量；`make run` 已带默认值，并使用当前 shell 中导出的变量，不单独加载 `.env`。

`SSRF_WHITELIST_EXTRA` 始终与设置页的 SSRF 白名单合并，设置页保存为空列表或修改白名单不会覆盖部署配置。要关闭 fake-ip 放行，应在部署环境中将该变量显式设为空，同时从设置页白名单移除自行添加的同一网段。


网页保留知识库、文章查看、实体档案、检索、收录审核与待审区、模型配置、队列、系统设置和 API Key 管理。CSV、Excel 收录摘要继续使用进程内 DuckDB 读取表结构和样本。

空间信息与 API Key 使用 `/api/v1/space` 及其子路径，系统设置与队列使用 `/api/v1/system/settings` 和 `/api/v1/system/runtime/queues`。已用空间继续统计，不设存储限额。

未设置 `SYSTEM_AES_KEY` 时，模型 API Key 与数据源凭据将以明文保存；启动日志输出警告，系统信息页显示凭据加密状态。设置有效的 32 字节密钥后按现有行为加密保存。

知识库置顶与资源收藏保存在个人空间中，登录用户和 API Key 看到相同结果。知识库列表中的复制只创建相同配置的空知识库，移动知识及其进度继续保留。登录后默认进入知识库列表。
