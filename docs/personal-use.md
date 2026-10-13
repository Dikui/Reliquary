# 个人使用指南

Reliquary 在一个个人空间的唯一库中收集、存储、去重与检索文章。综合分析和写报告由外部 AI 通过收录接口与 MCP 完成。统一术语和设计见 [CONTEXT.md](../CONTEXT.md) 与 [ADR 0009](adr/0009-kb-collect-store-retrieve.md)。

## 账号与网页入口

系统只有一个用户。首个用户注册成功后关闭注册，登录用户与有效 API Key 可以访问个人空间全部内容。API Key 支持名称、有效期和撤销，接入方式见 [收录接口说明](ingest-api.md)。

登录后进入文章页。主要入口为文章、知识检索、实体、主题、关注和设置。待处理页面分为收录待审与待补全，可以处理收录审核、修正内容类型，标记星标，修正内容类型和主题，并简单查看文章。实体页管理规范名、别名和合并；主题页管理名称、说明及历史文章归入进度。设置集中提供库、模型、文件库状态、操作日志、API 集成和运行队列。

用户可在页面修改密码。忘记密码时，保持数据库可用，在仓库根目录使用服务器相同的数据库配置执行：

```bash
go run ./cmd/server reset-password
```

新密码从标准输入读取，最少 8 个字符，UTF-8 编码不超过 72 字节。修改会撤销旧登录令牌。Docker 部署可执行 `docker compose run --rm --no-deps app ./Reliquary reset-password`。

## 收集、审核与处理

新内容由外部工具通过收录接口送入。Reliquary 不采集 RSS，不按链接抓取原文、图片或文件，网页不提供网址导入、文件上传与手动录入创建入口。外部工具的材料要求、HTTP 与 MCP 用法见 [收录接口说明](ingest-api.md)。正文中的图片需要由调用方上传。缺失图片显示来源链接，不由服务端下载；待补全清单仅记录缺失图片。

新材料进入收录审核流程，未通过的内容进入收录待审，审核通过后才写入文件库；待审正文及附件也进入隐藏维护副本。调用方自身的判断不会替代模型审核。缺正文或缺来源证据时先待审；重复与已丢弃的材料按收录结果处理。文章和本人文档缺文件或图片时正常入库并标记待补全。文章用同一原文链接再次收录，本人文档用文档 ID 再次收录。用户可在待处理页面接受缺失或丢弃内容。旧文章重新处理时缺正文，记录“重新处理需要正文”并等待外部工具补全，不抓取原网址。历史 RSS、网页和手动录入文章继续可读，手动录入专用编辑入口已删除。

规范化网址、正文哈希与近似重复检测用于去重，转载只补充来源记录。一篇文章通过一次处理调用得到内容类型、统一摘要、实体和主题。模型拿不准时归为“其他”，所有类型都全文保存。修正后的类型记为用户修正，操作记录日志。摘要包含一句话概述、3 到 5 条要点与关键名词。实体归一到实体档案，主题由用户定义，一篇文章可以属于多个主题。新建主题或修改说明后，可用历史摘要批量归入，页面显示进度。

| 内容类型 | 处理方式 |
| --- | --- |
| 所有内容类型 | Markdown 正文和图片全文保存 |

处理模型支持视觉时，在同一次调用中读取正文图片。生成模型支持 OpenAI 兼容、Anthropic 原生与 Ollama 接口。嵌入支持 OpenAI 兼容与 Ollama；重排支持通用 `/rerank` 格式及阿里云百炼接口。在设置中测试模型连接、维度、视觉支持与实际调用，再配置库的处理和嵌入模型。

文章未提供原文链接时，先尝试从材料本地提取链接，不联网搜索。仍未找到时进入收录待审，原因为“缺少原始链接”。本人文档通过 HTTP 或 MCP 收录时指定 `kind=mine`，仍经过模型收录审核与处理，审核标准保持一致；原始链接可以为空，不提取链接，不因缺链接进入待审。一律全文保存，教程、案例、参考类型也保留正文、图片与附件。与其他用户文档按现有规则去重，不与文章做转载归并。

缺链接的内容可补链接后重新审核，或确认无链接入库。无链接入库跳过模型审核，标记 source_unverified=true，始终全文保存，表头 url 为空；文章列表、详情与检索结果显示“来源未核实”，检索接口与 MCP 也返回该字段。收录待审中的本人文档显示“本人文档”，批准后进入同一路径。

## 文件库与检索

Compose 将宿主机 `RELIQUARY_VAULT_DIR` 绑定到容器内的 `VAULT_ROOT`。默认文件库为 `~/Reliquary/vault`，上传资源为 `~/Reliquary/files`，备份为 `~/Backups/Reliquary`。三者均可在 `.env` 修改为其他绝对路径。文件库内的目录结构保持如下：

| 目录 | 内容与使用方式 |
| --- | --- |
| `articles/年-月/` | 已批准文章的 Markdown 表头与正文 |
| `attachments/<id>/` | 全文文章和用户文档的图片 |
| `mine/` | 收录接口导入的本人文档，程序写入表头与全文，不监听文件变化 |
| `reports/` | 外部 AI 写入的整理结果，可被检索 |
| `.reliquary/` | 数据库维护副本，由程序生成 |

在 Obsidian 选择“打开文件夹作为仓库”，打开 `vault/` 这一层，例如 `~/Reliquary/vault`。建议在 Obsidian 设置的“文件与链接 > 排除的文件”中加入 `attachments/` 与 `.reliquary/`，避免搜索附件和维护副本。正文图片使用相对路径指向 `attachments/<文章id>/`，可直接显示。

文章和用户文档的正文在补全完成或用户接受缺失后定稿，不要在 Obsidian 修改正文、表头或 `.reliquary/`。用户文档不提供编辑，直接在 `mine/` 新增或修改文件不会更新索引。需要修改时，先在文章列表筛选“用户文档”后删除，或在详情中删除，再重新导入；HTTP 与 MCP 也提供按文档 ID 删除，删除会清理文件、附件和索引，并记录操作日志，相同内容可以重新导入。表头由数据库单向生成，手动修改会被下一次字段更新覆盖。星标、收藏理由、内容类型、实体与主题以数据库为准。上传和解析过程使用容器内 `LOCAL_STORAGE_BASE_DIR`，对应宿主机 `RELIQUARY_FILES_DIR`，备份同时包含此目录。

Obsidian 的 `.obsidian/` 阅读配置、`.trash/` 和 `.DS_Store` 等用户配置不会参与文件库核对、重建、`reports/` 监听或索引，程序不会修改或删除它们。备份和迁移包包含这些文件，保留阅读配置；恢复核对不比较其内容。

`VAULT_HEADER_DEBOUNCE` 合并表头连续更新，默认 `5s`。用户文档不监听文件变化，也没有索引等待时间设置。文件库状态页显示目录、最近扫描、文件数量与错误。

知识检索覆盖文章表头、全文正文、用户文档和整理结果，使用 PostgreSQL 全文与向量混合检索，按文章合并命中片段。可按内容类型、实体、主题、发布时间、星标及来源目录筛选。按发布时间筛选会排除缺少发布时间的文章并说明数量，采集时间不会代替发布时间。相似报道按摘要向量和时间窗口折叠，可以展开。

只运行一个后端进程。模型并发、速率和接口限流由该进程维护；后台任务与业务数据在同一 PostgreSQL 事务中持久化，设置保存后立即刷新进程内缓存。数据库备份同时包含未完成任务，恢复后任务可继续执行。文件库与数据库仍需一致性备份，迁移前应停止写入并等待运行队列完成。详见 [ADR 0010](adr/0010-postgres-task-queue.md)。

## 使用 ChatGPT 订阅

Reliquary 可以通过自己的 OpenID Connect 登录，使用符合条件的 ChatGPT Plus 或 Pro 账号完成收录审核、文章处理、主题历史归入和实体判断。此方式适用于开源、本地部署应用，具体模型和权限以账号授权结果为准。它不会读取 ChatGPT 对话，也不会读取 Codex 的本地凭据。Embedding 和 rerank 继续配置其他模型来源。参考 [官方快速开始](https://developers.openai.com/siwc/quickstart) 和 [开源应用说明](https://developers.openai.com/siwc/token-sharing-open-source)。

1. 在 `.env` 设置恰好 32 字节的 `SYSTEM_AES_KEY`。访问令牌、刷新令牌和保留的 ID token 都以 AES-256-GCM 加密后存入 PostgreSQL，没有明文降级。维护命令不把这个密钥或数据库中的加密凭据写入备份或迁移包。恢复时生成新密钥，按清单重新授权 ChatGPT 和填写其他凭据。
2. 配置 `CHATGPT_CALLBACK_URL=http://127.0.0.1:8080/auth/callback`，其中 `8080` 应与 Docker Compose 的 `APP_PORT` 后端映射端口一致。回调通过宿主机浏览器访问映射端口进入容器中已启动的后端；容器监听地址无需改成回环地址。回调必须使用 `127.0.0.1`，不能替换为 `localhost`，路径固定 `/auth/callback`。浏览器应运行在这台 Mac mini 上；其他电脑的 `127.0.0.1` 指向它自己，如需跨电脑操作，先建立访问该后端端口的本地 SSH 隧道。`CHATGPT_SETTINGS_URL` 设置成功页面返回模型设置的链接，默认前端地址为 `http://127.0.0.1/platform/settings?section=models`，前端端口变化时同步修改。参考 [官方登录流程](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)。
3. 在现有 SSRF 白名单配置中保留已用条目，并追加 `auth.openai.com,api.openai.com`。例如已有本地 Ollama 白名单时设置 `SSRF_WHITELIST=host.docker.internal,auth.openai.com,api.openai.com`。授权发现、JWKS、令牌刷新和撤销使用 `auth.openai.com`，模型目录和 Responses 使用 `api.openai.com`；所有请求沿用现有 SSRF 防护。
4. 重启后端，进入“设置 → 模型设置”，点击 **Continue with ChatGPT**。在新标签页完成账号、工作区和使用订阅额度的授权，确认回调页成功，再回到原设置页。首次使用会显示订阅用量确认。首次注册的客户端 ID 与部署主机 ID 都会保存，重启和再次登录继续使用；多个账号注册分别显示和保存。
5. 新建聊天模型，选择“用 ChatGPT 登录”，选择已登录的账号和从该账号目录加载的模型。显示名称来自服务端 `display_name`，调用使用 `slug`。将模型分配给生成、轻量审核或实体判断层级。选中支持图片的模型时，可以启用现有视觉能力选项。参考 [模型与推理](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference) 和 [预览限制](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)。

设置中可配置每日调用次数上限，留空表示不限。此计数统计 Reliquary 的生成调用尝试，失败也会计入；每天 UTC 00:00 清零，北京时间为 08:00，不是 OpenAI 的 token 余额。达到上限后暂停，次日自动恢复，也可提高或移除上限。手动恢复不会清零今日次数。

OpenAI 返回 `subscription_sharing_usage_limit_exceeded` 时，设置与运行队列显示额度暂停，并提供 **Manage usage** 链接到 [ChatGPT 用量设置](https://chatgpt.com/settings/usage)。该错误可能对应应用上限，不能仅凭错误码推断全部订阅耗尽或具体重置时间；调整额度或等待官方用量页显示恢复后，点击“手动恢复”。`subscription_sharing_usage_unavailable` 表示暂时无法确认用量，保留凭据并在五分钟后再试。暂停期间未完成的任务延后执行，额度暂停不消耗普通失败重试次数，文章的原始内容保留，主题批次保持待处理。参考 [错误与恢复](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery)。

修改文章内容类型或处理复核中的摘要转全文操作保持同步确认语义。生成暂停时返回可重试的 HTTP 503 与 `Retry-After`，保留原文章、分类、索引和复核状态；恢复后需要用户再次发起操作，系统不会稍后自动应用这次选择。

手动恢复解除生成暂停，已经延后的任务会在原定重试时间运行；需要立即重试文章时，在运行队列对相应任务点击“立即运行”。主题历史归入由后台轮询按批次恢复。

可明确选择一个已启用的本地 Ollama 聊天模型作为额度不足时的备用。配置后，暂停期间的调用使用该备用模型，包括触发额度错误的当前调用；没有备用时只暂停。备用模型同样需要具备任务所需的文本或图片能力。服务器不会自动改用其他付费 API。已发出的并发调用可能在暂停被记录前完成。

与 Codex 开发共用同一 ChatGPT 账号时，需要一起留意订阅用量。官方说明 Plus 的五小时额度在使用订阅的各应用之间共享，Pro 不适用这项五小时上限；各模型、应用及账号权限仍以用量页为准。Reliquary 的每日调用次数限制只限制本应用，不能显示或控制 Codex 的用量。参考 [账号、会话与用量](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)。

访问令牌接近过期时自动刷新并原子保存轮换后的凭据。刷新失败会显示“需要重新登录”；临时网络或服务器失败保留加密凭据并允许之后重试，不可用的刷新令牌会清除。点击对应账号的 **Continue with ChatGPT** 重新授权，模型仍关联该账号注册。参考 [令牌说明](https://developers.openai.com/siwc/token-sharing-open-source/token-reference)。

退出时，在模型设置中点击该账号的“退出登录”。后端先尝试撤销可续期会话，再清除本地访问、刷新及 ID token，保留账号与客户端映射、主机 ID。若未确认远程撤销，界面会提示到 ChatGPT 设置断开应用。退出后该账号不再发起生成请求，重新登录后可继续使用原模型配置。

## 关注与简报

在实体页、主题页，或文章详情的实体与主题标签旁点击“关注”，再次点击即可取消。关注与取消关注会写入操作日志。文章星标继续作为书签，可独立使用。

侧栏“关注”位于主题之后，显示未看新文章数。打开关注动态可集中查看与任一关注对象关联的已完成文章，按收录时间倒序，每篇显示命中的实体与主题；可以按对象筛选，也可以开关相似报道折叠并展开报道。用户文档、整理结果、收录待审内容不出现在动态中。

“新”根据进入动态时间判定：取文章处理完成且满足动态资格的时间与实体、主题关联建立时间的较晚者，多个命中取最新时间。晚于上次成功打开关注动态的文章标为新，因此查看时仍在处理的文章，完成后仍能显示为新。新增关注不重置旧文章的进入时间。主题历史归入单独标记为历史关联，不产生新动态，只在历史列表展示；首次打开前，其余符合条件的文章视为新。首次加载成功后记录本次查看时间，本次显示的新标识仍保留，之后进入动态的文章继续计入侧栏数字。“全部标为已看”更新查看时间并清除当前新标识。数字按文章去重，与相似报道是否折叠无关。实体合并时关注转移到保留实体并去重，保留最早关注时间；删除主题会删除该主题的关注。

Reliquary 提供资料，不生成周报或推送通知。下面是外部 AI 通过已配置 MCP 编写周报的示例流程，只用于说明工具用法，没有内置 skill：

1. 调用 `list_follows`，读取关注对象的 `target_type`、`target_id`、名称、关注时间和近 7 天文章数，确认本周关注方向。
2. 调用 `follow_feed`，显式指定周报开始时间，例如 `{"since":"2026-10-05T00:00:00+08:00","limit":100,"collapse_similar":true}`。只整理某些对象时，增加 `target_ids`。省略 `since` 使用网页上次查看时间；MCP 调用不会更新该时间。since 比较进入动态时间，排序仍按收录时间倒序。每次收到 `next_cursor`，继续调用 `follow_feed`，传入 `{"cursor":"上一页的 next_cursor","limit":100}`，直到响应没有 `next_cursor`；再开始整理全部主项及折叠成员。
3. 根据返回的 `results`、`matched_follows` 和 `similar_reports` 归纳线索。需核实的全文文章用 `get_article` 分段读取。文章 ID 用于引用和去重，勿把相似报道当作独立事件重复统计。
4. 由外部 AI 写出 Markdown 周报，注明时间范围、来源文章与资料限制，再调用 `write_report` 保存，例如：

   ```json
   {
     "title": "关注周报 2026-10-05 至 2026-10-11",
     "markdown": "# 关注周报\n\n## 产品进展\n\n根据本周原文归纳进展，并逐条注明来源文章。\n",
     "related_article_ids": ["替换为本周引用的真实文章 ID"]
   }
   ```

`follow_feed` 默认返回最多 20 个主项，`limit` 最大为 100；相似报道作为主项的 `similar_reports` 返回，不占主项额度。`total` 是首请求窗口内折叠前的候选文章数。游标保留首请求的时间上限、since、对象筛选和折叠开关，后续可只传 cursor 与 limit。必须翻页直到没有 next_cursor，再按文章 ID 汇总主项和折叠成员。整理结果写入 `reports/` 后可检索，仍不会进入关注动态。

## 部署在 Mac mini

Mac mini 使用 Apple Silicon，安装 Git、Node.js/npm 和支持 Docker Compose 的 Docker 兼容运行环境（如 OrbStack、Docker Desktop）。应用与 PostgreSQL（ParadeDB）使用 Compose；模型服务可以在 macOS 原生运行。应用镜像从当前仓库构建，未发布公共应用镜像。

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env
```

编辑 `.env`，至少确认以下配置：

| 配置 | 说明 |
| --- | --- |
| `RELIQUARY_VAULT_DIR` | 宿主机文件库目录，默认 `${HOME}/Reliquary/vault` |
| `RELIQUARY_FILES_DIR` | 宿主机上传资源目录，默认 `${HOME}/Reliquary/files` |
| `BACKUP_DIR` | 宿主机备份目录默认值，默认 `${HOME}/Backups/Reliquary`；计划可在设置中修改 |
| `VAULT_ROOT` | 容器内文件库路径，建议保持 `/data/vault` |
| `LOCAL_STORAGE_BASE_DIR` | 容器内上传资源目录，建议保持 `/data/files` |
| `DB_DRIVER`、`DB_HOST`、`DB_PORT` | 使用 `postgres`、Compose 服务名 `postgres` 与 `5432` |
| `DB_USER`、`DB_PASSWORD`、`DB_NAME` | PostgreSQL 账号、密码和库名，同时用于初始化数据库服务 |
| `JWT_SECRET`、`SYSTEM_AES_KEY` | 设置自己的登录签名密钥及 32 字节凭据加密密钥；迁移和整体恢复时由维护命令生成新值 |
| `OLLAMA_BASE_URL` | 容器访问 macOS 模型服务的地址，例如 `http://host.docker.internal:11434` |
| `OLLAMA_OPTIONAL` | 设为 `true` 时，Ollama 暂不可用只输出告警 |
| `SSRF_WHITELIST_EXTRA` | 部署额外放行的模型或解析服务主机/IP/CIDR；与设置页白名单合并 |
| `APP_PORT`、`FRONTEND_PORT` | 宿主机 API 与网页端口，默认 `8080` 和 `80` |
| `APP_EXTERNAL_URL` | 需要外部调用方读取限时文件链接时填写内网访问地址并代理 `/r/` |

模型服务的完整地址、模型名、API Key、嵌入维度在“设置 > 模型”配置，库页选择具体档位。也可通过 [内置模型配置](./builtin-models.md) 声明。迁移后需要检查数据库中保存的模型地址是否仍可访问。

Compose 默认直接绑定宿主机目录，无需另写覆盖文件。`.env` 中的默认配置如下，可按需要修改位置，路径含空格时用引号包住值：

```dotenv
RELIQUARY_VAULT_DIR=${HOME}/Reliquary/vault
RELIQUARY_FILES_DIR=${HOME}/Reliquary/files
BACKUP_DIR=${HOME}/Backups/Reliquary
VAULT_ROOT=/data/vault
LOCAL_STORAGE_BASE_DIR=/data/files
```

启动前执行部署检查。不存在的目录会创建；目录不可写、位于项目源码内，或文件库、上传资源、备份目录相同或互相包含时拒绝启动。检查按实际路径处理符号链接。备份目录同时检查 `.env` 的默认值和数据库中已保存的设置。源码与数据应使用分开的目录，例如源码在 `~/Projects/Reliquary`，数据在 `~/Reliquary/`。

文件库、上传资源、备份目录不要放在“桌面”或“文稿”目录下。

检查和启动使用以下命令；数据库启动后再检查一次，可以读取已有的备份设置：

```bash
export COMPOSE_PROJECT_NAME=reliquary
./reliquary-admin check-deployment
./scripts/build-frontend-dist.sh
docker compose build
docker compose up -d --wait postgres
./reliquary-admin check-deployment
docker compose up -d
```

`./scripts/start-all.sh --docker` 也会在应用启动前创建并检查宿主机目录，先启动数据库，再核对已保存的备份设置。应用入口随后为挂载目录设置容器用户写入权限，并以 `appuser` 运行。在 macOS 的 Docker 兼容运行环境中，宿主机登录用户仍可读取文件库供 Obsidian 使用；无需手工执行递归 `chown`。运行环境的文件共享设置应允许访问所选目录。

启动后访问 `.env` 配置的网页端口，注册唯一用户，配置模型并收录一篇文章。检查宿主机文件库是否产生 Markdown 和附件，知识检索是否命中。后续执行 Compose、备份或恢复时，使用相同的 `COMPOSE_FILE` 和 `COMPOSE_PROJECT_NAME`。数据库仍使用原有命名卷；不迁移旧的文件库或上传资源命名卷。

推荐将 Ollama 直接安装到 macOS，在宿主机运行生成和嵌入模型。Ollama 支持 Apple GPU 的 Metal 加速；其官方文档说明 macOS 上的 Docker 容器不能使用 GPU 加速。因此将模型服务放在 macOS，Compose 中只运行 Reliquary 与数据服务，不依赖 CUDA。参见 [Ollama 硬件支持](https://docs.ollama.com/gpu)与 [Ollama FAQ](https://docs.ollama.com/faq#how-do-i-use-ollama-with-gpu-acceleration-in-docker)。

若 Ollama 桌面应用仅监听回环地址，可按[官方环境变量说明](https://docs.ollama.com/faq#setting-environment-variables-on-mac)执行 `launchctl setenv OLLAMA_HOST "0.0.0.0:11434"` 并重启 Ollama，让 Docker 通过 `host.docker.internal:11434` 访问。使用命令行服务时可在单独终端执行 `OLLAMA_HOST=0.0.0.0:11434 ollama serve`。让此端口只在可信内网可达。选择能放入统一内存的模型，先测试文章处理与嵌入，再调整后台并发。

若模型连接被 SSRF 校验拒绝，可把实际使用的 `host.docker.internal` 或模型主机加入 `SSRF_WHITELIST_EXTRA` 后重启应用。Compose 默认额外放行 `198.18.0.0/15`，用于本机代理 fake-ip；没有此需求可在 `.env` 显式设置 `SSRF_WHITELIST_EXTRA=`。默认网段本身不会放行回环、私网或 Docker 服务名，额外配置应写明具体需要访问的主机。

### 多模态 embedding 与全部向量重建

文字与图片必须由同一个 embedding 模型生成，维度相同不足以说明向量兼容。可选的 [本地服务](../tools/mm-embed-server/README.md) 使用 transformers，默认运行 Apache 2.0 许可的 `google/siglip2-base-patch16-224`，完整模型约 375M 参数，768 维。它支持 MPS 和 CPU，权重放仓库外缓存，Python 包只安装到虚拟环境：

```sh
python3 -m venv .scratch/kb-reshape/evidence/12/.venv
.scratch/kb-reshape/evidence/12/.venv/bin/python -m pip install -r tools/mm-embed-server/requirements.txt
HF_HOME="$HOME/.cache/reliquary-mm-models" \
  .scratch/kb-reshape/evidence/12/.venv/bin/python tools/mm-embed-server/server.py \
  --model google/siglip2-base-patch16-224 --device auto --port 18098
```

原生后端访问 `http://127.0.0.1:18098/v1`。容器访问宿主机时，服务增加 `--host 0.0.0.0`，配置地址为 `http://host.docker.internal:18098/v1`，并用既有 `SSRF_WHITELIST_EXTRA` 放行这个主机。服务仅部署在可信内网；默认回环监听可用 Ctrl+C 停止。

在“设置 > 模型”新增或编辑 embedding 模型，选择自定义 HTTP 服务，填写模型名、地址和维度，开启“支持图片”，请求格式选 Jina。虽然服务在本机，连接方式使用远程 HTTP 模型配置。然后在“设置 > 库”的模型页签选择该 embedding 模型。无需改变文章处理模型。资料的正文图片会生成图片向量，搜索命中时显示通过附件鉴权加载的缩略图，用户文档生成正文分块、标题与摘要和正文图片向量，整理结果只生成文字分块向量。本地服务不下载输入图片 URL，应用传入已保存附件的 data URI。

图片通道沿用 RRF，默认权重 0.2，正文分块和文章向量通道各为 0.35，关键词为 0.3。权重无需合计为 1；图片用于补充召回，避免较粗的图像相似度压过匹配正文。在检索参数设置中可修改图片权重，填 0 使用默认 0.2；模型没有开启支持图片时通道关闭。

模型换成较大型号时，先在新服务确认文字和图片均返回同一模型的向量及正确维度，再创建新的 embedding 模型记录并选择它。使用新记录可明确区分模型身份，避免直接覆盖相同 ID 的权重。`--model` 可替换成其他具有 transformers 双塔特征接口的模型；其他架构需使用支持它的 Jina 请求格式服务，本地小服务不支持任意模型架构。

模型选择后旧模型向量立即退出向量召回，关键词检索持续可用，新向量逐项完成后加入召回。进入“设置 > 库”的模型页签，点击“重建全部向量”，后台显示总数、已完成和失败数。重建覆盖正文分块、文章标题与摘要、正文图片、用户文档与整理结果的分块、实体名称。可暂停后继续，服务重启后保留检查点；失败列表显示错误，可只重试失败项。已成功的检查点不会重复计算。模型配置变化后的旧任务不能继续，需用当前模型开始新重建。

命令行入口沿用 server 的数据库、模型、文件库和白名单配置。原生运行示例：

```sh
go run ./cmd/server reembed
go run ./cmd/server reembed --resume <任务ID>
go run ./cmd/server reembed --retry-failed <任务ID>
```

命令以 JSONL 输出进度，Ctrl+C 保存暂停状态。Compose 部署可在应用容器执行同一 `server reembed` 子命令。界面与命令行共用任务表，应继续已有任务，避免同时创建重建。

第一次启动包含约 1.5 GB 权重下载和加载时间。本机 Apple M4、16 GB 内存的 CPU 实测中，预热后的短文字约 26 ms、单图约 63 ms，28 个混合向量后台重建约 2 到 5 秒，峰值 RSS 约 1.5 GiB。相同规模输入的 1000 个向量可先按 1 到 3 分钟估计，长文窗口、其他进程负载、模型限流和网络会延长时间。完整收录、检索、重建记录见 `.scratch/kb-reshape/report-12.md`；不同模型和设备耗时不同。重建单位是向量数，长文章有多个分块，不能直接用文章数估算。可以用一小批实际数据的单项耗时乘以页面总数估计，再留出模型限流、磁盘读取和失败重试时间。默认模型文字上下文仅 64 tokens，本地服务对长文字分窗口编码并聚合，这只用于验证链路，未进行模型质量评测。

## 维护入口与日常核对

在部署源码目录使用 `./reliquary-admin`，宿主机需要 Python 3、Git、Docker 和 Docker Compose。它统一调用服务器维护子命令和 Docker Compose。保留 Compose 多服务，应用镜像按版本或提交标识，依赖镜像固定版本及摘要。操作时保持相同的 `COMPOSE_FILE` 和 `COMPOSE_PROJECT_NAME`，不要同时运行其他维护命令。`./reliquary-admin --help` 和各子命令的 `--help` 显示全部选项。

文章表头和文件库根目录的 `.reliquary/` 是数据库的可读副本。档案、主题及关联来源、关注和查看时间、处理复核、收录待审原文及附件、转载、操作日志、用户修正标志、收录附加字段和不含凭据的配置均有对应文件。变化后按表头相同的去抖时间写入，同目录原子替换；操作日志正常同步只追加 JSONL。文件格式和全部表列的归属见 [维护数据归属](maintenance-data.md)。不要在 Obsidian 修改这些副本。

```bash
./reliquary-admin verify
./reliquary-admin verify --repair
```

第一条输出逐项差异，成功后应没有未解决的差异。第二条根据数据库补回文章、用户文档文件和数据库副本，再核对；`reports/` 原文由文件库备份恢复。错误时保留原库和故障文件，先检查输出指向的路径、权限和磁盘空间，再从完整备份恢复。不要通过删除数据库行消除差异。

## 每周备份和每月恢复演练

备份目录默认 `~/Backups/Reliquary`。首次部署可在 `.env` 的 `BACKUP_DIR` 设置默认值；已有计划在设置中或通过 `schedule configure --backup-directory` 修改，例如改为外接硬盘路径。它必须位于项目源码之外，且与文件库、上传资源目录互不包含。修改后运行 `./reliquary-admin check-deployment` 检查。外接硬盘到位前，这种备份只防止误删、应用损坏和升级失败；同一台机器的硬盘损坏仍可能同时损坏原件和备份，这个风险已接受。

在运行正常部署的同一账号终端配置计划，再安装宿主机调度：

```bash
./reliquary-admin schedule configure \
  --backup-directory /Users/yourname/Backups/Reliquary \
  --backup-schedule '0 3 * * 0' \
  --drill-schedule '0 4 1 * *' \
  --timezone Asia/Shanghai --enabled true
./reliquary-admin schedule install
```

第一条返回保存的目录、计划和时区。五个 cron 字段依次是分钟、小时、日期、月份和星期，上例每周日 03:00 备份、每月 1 日 04:00 演练。第二条应显示调度已安装，配置和日志保存在部署目录的 `.maintenance-schedule/`；macOS 使用当前登录用户的 launchd，Linux 使用用户 systemd。Docker 必须保持可用。调度器每分钟检查一次，首次运行立即备份和演练，主机休眠错过计划后补跑一次；失败、超时或中断的自动作业十五分钟后重试。换部署目录或机器后重新安装调度，不把旧机器的计划当成新机器已安装。暂停自动运行可执行 `./reliquary-admin schedule configure --enabled false`。

每份备份完整导出数据库；宿主机文件库和上传资源按增量快照复制，未变化文件用硬链接，每份目录都能单独恢复。文件库内 `.obsidian/` 等阅读配置一同保留，备份核对不比较这些用户配置。只保留最近 4 份正常完成的定期备份，不把失败的半成品作为成功备份。系统加密密钥、JWT 密钥、数据库密码、API Key、模型凭据、ChatGPT 令牌不进入备份。唯一账号的密码哈希保留，恢复后仍用原账号密码登录。

备份暂停 HTTP/MCP 收录及附件上传，等待短写入事务及文件副本写入结束，再停止应用取得一致快照。它不等待主题历史归入、向量重建等长任务完成；队列和检查点一同导出。恢复后 PostgreSQL 队列重新领取中断任务，保留文章与重试预算。等待超过上限时跳过本次备份，恢复收录并记录告警，不生成假成功结果。请暂停 Obsidian 等外部编辑器写文件，程序无法锁住其他软件。

手动触发和检查自动计划使用以下命令。手动备份的目录参数明确指定位置，避免与 `.env` 中的 `BACKUP_DIR` 默认值混淆：

```bash
./reliquary-admin backup /Users/yourname/Backups/Reliquary --timeout 300
./reliquary-admin drill /Users/yourname/Backups/Reliquary
./reliquary-admin schedule run --once
```

备份成功应显示 `backup complete and read-back verified` 和完整备份路径，设置中的文件库状态区域显示最近一次备份时间、大小、状态和错误，也显示最近一次恢复演练结果。`schedule run --once` 只运行当前到期的计划，已有其他 runner 时跳过，手动迁移暂停时也不运行；演练中可加 `--force` 强制运行两项，仍遵守暂停及自动运行开关。

每月演练将最近一份备份恢复到隔离的临时应用容器、空数据库和临时目录，重建全文索引并运行 `verify`。通过后应显示 `Monthly restore drill passed`，临时数据库、文件和容器已删除。失败也清理临时环境，保留设置页错误和调度日志并告警。例行演练不需要访问原模型或调用付费模型，只核对不可再生数据和文件；模型可用性由 `setup-models` 单独检查。演练清理不能影响正常部署。

普通备份默认不重复保存镜像。已有完整源码与固定版本镜像时，先准备新的空数据库，在目标 `.env` 的 `RELIQUARY_VAULT_DIR` 与 `RELIQUARY_FILES_DIR` 指定空或不存在的宿主机目录，保持目标应用停止，再在目标部署目录执行 `./reliquary-admin restore /完整备份路径`。非空目标目录会被拒绝。它恢复数据和阅读配置，并进入下文的模型及凭据等待流程，不会替换目标部署的代码。需要带代码及镜像的一体恢复包时执行 `backup /备份目录 --with-images`，再使用 `unpack` 恢复到空部署目录。

个人空间命名清理之前生成的版本 1 维护副本和备份也可在当前代码上恢复。恢复旧数据库后，维护入口保持暂停，先按原备份清单核对旧副本、附件和向量，再导出旧采集配置、运行新增迁移、重建全文索引和刷新当前副本；最后再次按未改写的原备份清单核对。副本旧表名及外层列名会映射为当前名字。未完成任务继续读取原 payload，模型及凭据检查通过后才能恢复执行。结构版本 0 的整体回退仍遵守下文的离线 helper 流程。

## 迁移到另一台 Mac mini

旧机器在当前部署目录执行：

```bash
./reliquary-admin pack /Volumes/Transfer/Reliquary-Migration
```

目标目录必须不存在或为空，而且不在文件库或上传资源目录内。命令暂停收录并等待普通处理队列清空，默认最多等待 300 秒，可用 `--timeout 600` 调整；主题历史归入和向量重建的检查点会保留。命令随后打包当前源码及 Git 历史、脱敏数据库导出、宿主机文件库（含 `.reliquary/` 和 `.obsidian/` 阅读配置）、上传资源、脱敏配置、所有 Compose 服务的镜像文件、版本信息、表级校验、文件哈希和固定查询结果。模型权重不打包。

完成前，命令读回磁盘文件并核对。只有所有核对通过才报告 `pack complete and read-back verified` 并恢复旧环境的收录；`--keep-paused` 会保留暂停并停止应用。等待超时、文件变化、磁盘不足或任何校验错误会停止并给出原因，不能把这样的目录用于迁移。打包期间暂停外部编辑器写文件。未提交的源码默认拒绝打包，需要保留经审阅的本地修改时显式加 `--include-worktree`，清单会记录提交和工作树状态。

把整个目录复制到外接硬盘，在新机器进入该目录运行随包提供的入口：

```bash
./reliquary-admin unpack
```

命令检查 Docker、Compose、端口、磁盘空间和目标目录，恢复源码与 Git 历史并载入镜像；生成新 `SYSTEM_AES_KEY`、`JWT_SECRET` 和数据库密码，写入权限受限的新 `.env`。数据库、文件库、阅读配置和上传资源一同恢复，并重建全文索引。宿主机文件库与上传资源目录必须为空或不存在，可用 `RELIQUARY_VAULT_DIR`、`RELIQUARY_FILES_DIR` 指定新位置；非空时拒绝恢复。容器中的文件库和资源路径应沿用包的配置，命令按需要映射恢复文件引用。

默认的新部署目录是迁移包同级的 `包名-deployment`，可用 `--target /Users/yourname/Reliquary-Deployment --project reliquary-new` 显式指定空目录及独立 Compose project。数据目录位于部署源码目录之外。需要同时更换端口和数据位置时，在打包目录运行：

```bash
RELIQUARY_VAULT_DIR=/Users/yourname/Reliquary-New/vault \
RELIQUARY_FILES_DIR=/Users/yourname/Reliquary-New/files \
BACKUP_DIR=/Users/yourname/Backups/Reliquary-New \
APP_PORT=18080 FRONTEND_PORT=18081 \
  ./reliquary-admin unpack --target /Users/yourname/Reliquary-Deployment --project reliquary-new
```

后续维护命令在新部署目录执行，保留生成的 `.env` 和 Compose 映射。

接下来必须看到核对结果和 `Reconfiguration checklist`。缺少本地模型时状态为 `waiting_models`，等待模型就绪；已选择的模型缺少凭据时为 `waiting_credentials`，等待重新填写。两种状态都会启动网页供登录和修改设置，但暂停收录与领取处理任务，保留所有中断任务。命令因等待返回非零状态是预期行为；不要通过手工 `maintenance resume` 跳过检查，也不要重复执行初次 `unpack` 覆盖已恢复的数据库。

按清单重新填写模型 API Key、App Secret、鉴权请求头，URL 中的认证参数也要补齐；ChatGPT 账号重新登录，回到模型列表刷新并测试。模型所需字段全部补齐后才解除该模型的维护暂停，只有 API Key 而仍缺 App Secret 或请求头时继续暂停。收录 API Key 重新生成并更新 HTTP/MCP 调用方。自动生成的系统密钥和数据库密码无需填入旧值。

检查模型地址、端口、SSRF 白名单、外部资源链接地址、ChatGPT 回调与设置页地址、备份位置及自动调度。每项配置完成前，对应模型、解析或收录功能保持暂停。在新部署目录执行：

```bash
./reliquary-admin setup-models --install --download --start
./reliquary-admin unpack --resume
```

第一条安装或下载缺少的本地模型并试调用；已就绪时可省略安装选项。第二条再次检查所选模型、凭据和文件，并默认在新部署目录安装本机自动备份与恢复演练调度。全部通过才显示 `State: ready` 并恢复队列与收录。不影响当前所选模型的其他未配置功能仍暂停，可之后逐项配置。在“设置 → 文件库 → 备份与恢复”查看目录及周月计划，使用 `schedule configure` 修改。

本机调度安装失败时显示 `waiting_scheduler`，应用保持维护暂停。检查 launchd 或用户 systemd 错误，修复后再运行 `unpack --resume`。`--no-scheduler` 仅用于隔离演练，防止演练环境注册本机计划；日常部署沿用默认行为。本任务的验证环境使用此选项，没有给开发机器安装自动计划。

若新环境使用不同 embedding 模型，先选择并测试新模型，再运行 `unpack --resume`。程序自动以维护参数执行任务 12 的全部向量重建，覆盖正文分块、文章、图片、用户文档、整理结果及实体名称，保留实体档案时间，然后核对并开放使用。关键词索引仍可重建，不要求模型在线。向量本身和检索分数是可重新生成的数据，核对规则分别检查模型身份、维度、完整性和固定查询命中文章；不可再生数据逐表精确核对，不用分数误差放宽原文和用户判断。

任何文件或数据不一致都退出失败，保持维护暂停，不进入正常使用。修复后重新核对，禁止把“程序能启动”当成迁移成功。

## 只有文件库时重建

保留整个宿主机文件库，包括隐藏的 `.reliquary/`、`attachments/` 和 Obsidian 阅读配置。准备新的空数据库和空上传目录，在 `.env` 将 `RELIQUARY_VAULT_DIR` 指向原件目录，例如 `/Users/yourname/RecoveredVault`，并设置 `RELIQUARY_FILES_DIR`，保持应用停止，执行：

```bash
./reliquary-admin rebuild /Users/yourname/RecoveredVault
```

命令读取表头、正文和 `.reliquary/`，恢复文章、带表头的用户文档及表头未保存的用户数据、档案、待审内容、日志、配置和任务检查点，再生成全文索引并逐表核对原副本。`.obsidian/`、`.trash/` 和 `.DS_Store` 等用户配置保留原样，不作为文章或用户文档索引。重建成功后自动进入 snapshot 暂停，队列不会领取任务或消耗重试预算。缺少或损坏的副本、文章、待审附件会列出错误并停止。`mine/` 中没有表头的文件会跳过并列入输出，不迁移旧文件或用户手放的文件。可重建的索引和向量不作为唯一原件。

随后可以执行 `docker compose up -d app frontend` 启动网页，用原账号密码登录。按照程序输出的清单重新填写模型凭据，ChatGPT 重新登录；账号密码哈希保留。此时处理队列与收录仍暂停，不要手工解除暂停。完成后执行：

```bash
./reliquary-admin setup-models
./reliquary-admin rebuild /Users/yourname/RecoveredVault --resume
```

第一条确认配置模型可用；第二条只继续已经重建的库，读取初次保存的固定核对清单，不再次导入数据。它先严格核对，再检查模型及必需凭据，自动补齐全部向量，保留实体档案时间，核对向量完整性并安装本机备份调度。缺模型、缺凭据、向量失败、核对不一致或调度安装失败时继续暂停并返回非零状态。全部通过才显示 `State: ready`、解除维护暂停并让原有任务自动继续。其他未配置功能仍暂停。隔离演练可加 `--no-scheduler`，日常使用默认安装。初次清单保存在部署目录的 `.maintenance-rebuild.json` 等维护状态文件中，位于文件库之外，不进入迁移包。不要删除清单或重新执行初次 rebuild 来跳过失败。

仅有任务 15 之前的文件库且没有 `.reliquary/` 时，旧 `vault-rebuild` 路径只能恢复表头和正文，没有对应副本的档案说明、日志、关注、待审及配置不能凭空恢复。保留原件和输出的跳过清单，寻找数据库备份，不要宣称完整重建成功。

文章或用户文档文件损坏时，数据库中的正文副本可恢复 `articles/` 和 `mine/` 文件。`reports/` 的原文只靠文件库备份恢复，数据库的索引不是原文备份；最多丢失上次成功备份后的改动。其他软件修改或删除整理结果文件时也受这个限制。

## 模型准备

```bash
./reliquary-admin setup-models
```

先检查 Ollama 是否存在、配置中的模型是否下载，以及多模态 embedding 服务是否可调用，最后对可用的配置模型各试调用一次。缺失时输出可执行的辅助命令；`setup-models --install --download --start` 使用官方安装方式安装 Ollama、显示下载进度并拉取缺失模型，为多模态服务创建独立 Python 环境和下载权重，随后启动服务。安装流程依据 [Ollama macOS 官方说明](https://docs.ollama.com/macos)、[Linux 官方说明](https://docs.ollama.com/linux) 和 [模型下载命令](https://docs.ollama.com/cli)。macOS 安装 Ollama 时按系统管理员密码及 CLI 权限提示操作，Linux 官方安装程序可能要求 sudo；不把密码写到配置或日志。模型权重默认留在 `~/.cache/reliquary-models/` 等本机模型缓存中，可用 `RELIQUARY_MODEL_HOME` 更改多模态模型目录，不进入迁移包或备份。

没有所选模型或凭据时保持等待并返回非零状态。模型连通但名称、维度或文字与图片能力不符也不能视为就绪。填好凭据及模型之后重新运行检查和 `unpack --resume`。用 `--start` 启动的多模态服务会输出进程号及日志路径，停止时按输出执行 `kill 进程号`；重启机器后需按部署方式重新启动服务。

## 升级与失败退回

每次数据库结构变更都新增迁移文件，冻结 `000000_init`。规范与升级数据完整性测试要求见 [数据库迁移规范](database-migrations.md)。

```bash
./reliquary-admin upgrade
```

升级前自动产生整体备份，包括旧代码、旧镜像和旧数据，版本 4 及以前的完整备份仍包含 `data_sources` 与 `sync_logs`。执行 `000005` 前先将两表的非密钥内容导出到文件库 `.reliquary/exports/`，JSON 包含导出时间与迁移版本。导出失败则不执行迁移并保持暂停。旧备份恢复到当前版本时也先导出再迁移，已有文章、附件及历史渠道不删除。开发环境若直接运行迁移，必须先执行 `./reliquary-admin export-sources`。迁移完成后核对不可再生数据和文件，只有通过才恢复使用。失败时保持暂停并显示升级前快照位置。不要运行迁移 `down` 或修改 `schema_migrations` 来退回生产。

退回需要用升级前快照恢复旧代码、旧镜像、旧数据库和旧文件库到新的空环境。`upgrade` 的自动备份包含这些镜像，因此可在该备份目录执行随包提供的入口：

```bash
RELIQUARY_VAULT_DIR=/Users/yourname/Reliquary-Rollback/vault \
RELIQUARY_FILES_DIR=/Users/yourname/Reliquary-Rollback/files \
BACKUP_DIR=/Users/yourname/Backups/Reliquary-Rollback \
  ./reliquary-admin unpack --target /Users/yourname/Reliquary-Rollback-Deployment --project reliquary-rollback
```

如果旧部署占用默认端口，像迁移一样指定空闲的 `APP_PORT` 和 `FRONTEND_PORT`。应看到旧代码和镜像载入、数据核对通过以及重新填写凭据的清单；在回退部署目录补齐凭据、运行 `setup-models` 和 `unpack --resume`，确认 `State: ready` 后再切换使用旧版本。`restore` 只适用于已经准备好旧代码及旧镜像的空数据环境，不负责替换代码。升级以后新增的数据不在升级前备份中，先另存当前数据，不能直接覆盖。系统不支持数据库结构降级。


旧版 ChatGPT 通过离线维护入口重新登录，不导入旧令牌，也不用先升级数据库：

```bash
./reliquary-admin maintenance chatgpt-login --id 原账号ID --callback http://127.0.0.1:8080/auth/callback
./reliquary-admin maintenance chatgpt-complete --input /仅本机目录/chatgpt-callback.json
./reliquary-admin setup-models
./reliquary-admin unpack --resume
```

第一条返回短期 `authorization_url` 和到期时间，用户在自己的浏览器打开并授权。回调端口按旧部署的 `APP_PORT` 填写。旧应用停止时，授权后本地连接失败是预期行为；复制浏览器地址栏中的完整回调 URL，在权限为 0600 的本机文件中写一个 JSON 对象 `{"callback_url":"完整回调URL"}`，交给第二条。也可通过 stdin 传入，不将授权码放在命令参数、迁移包或备份目录。程序使用原 OAuth 的 state、PKCE、签名和身份校验，成功后显示 `connected` 和恢复的模型 ID；随后删除输入文件。授权过期或客户端变更时按输出的新链接重新授权。原账号、主机 ID、客户端及非秘密授权历史保留。最后两条应显示模型试调用通过和 `State: ready`，此时才启动旧应用。

维护期间读取 ChatGPT 配额只读原数据。真实模型试调用仍按原规则消耗配额；命令及设置页试调用将前后配额追加到操作日志。迁移核对只接受完整衔接的记录，原配额配置和既有日志仍须一致，不能通过跳过额度计数完成核对。


## 外部 Agent 接入

Grok Bot、Claude Code 等外部工具需能访问 Reliquary 的内网地址。登录网页后，在“设置 > API 集成”为每个工具创建自己的 API Key，复制完整令牌和连接地址。同页可查看 Key、修改名称与有效期或撤销 Key。

将 [收录接口说明](ingest-api.md) 提供给外部工具开发者或 Agent。该文档包含完整材料要求、认证、查重与收录顺序、可执行示例、HTTP 与 MCP 接口、补全、重试和用户反馈。外部工具负责取得原始资料，Reliquary 负责去重、模型审核、处理、存储与检索。收录结果和待处理页面供用户确认后续操作。
