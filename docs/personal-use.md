# 个人使用指南

日常正文保存在 PostgreSQL，图片等原始资源保存在磁盘。网页预览按需读取数据库并在关闭后释放，不生成 `.md`。设置 → 资料导出与备份 → 导出文件库，仅在需要离线阅读或文件库恢复时生成 Markdown 和 `.reliquary/`；旧导出文件不会自动删除。下文的 Obsidian 阅读及仅文件库重建说明适用于显式导出的文件库。

Reliquary 在一个个人空间中按库收录、存储、去重、检索资料，并维护实体关系图谱。系统允许零库，由用户按来源与可信度自行建库；所有库统一使用通用模板。综合分析和写报告由外部 AI 通过 HTTP 与 MCP 完成。统一术语和设计见 [CONTEXT.md](../CONTEXT.md) 与 [ADR 0014](adr/0014-internal-knowledge-base.md)。

## 账号与网页入口

系统只有一个用户，没有登录。网页和后端端口只绑定本机 `127.0.0.1`，打开网页即可访问个人空间全部内容；外部工具使用 API Key。其他设备无法访问，见 [ADR 0015](adr/0015-no-login-local-only.md)。API Key 支持名称、有效期和撤销，接入方式见 [收录接口说明](ingest-api.md)。

打开网页后进入资料页。主要入口为资料、检索、实体、主题、关注、测评和设置。待处理页面只保留收录待审，可审核或丢弃。资料可标星、修正内容类型与主题，实体页可查看关系来源、删除关系和合并实体。设置提供资料库设置、新建通用资料库、模型、文件存档与备份、操作日志、API 集成、运行队列和调用统计。测评页管理题集、确认 AI 候选、运行普通或图谱检索评测并比较两次完成运行。

## 收集、审核与处理

新内容由外部工具通过收录接口送入。Reliquary 不采集 RSS，不按链接抓取原文、图片或文件，网页不提供网址导入、文件上传与手动录入创建入口。外部工具的材料要求、HTTP 与 MCP 用法见 [收录接口说明](ingest-api.md)。正文中的图片需要由调用方上传。缺失图片显示来源链接，不由服务端下载，不产生缺图待办。

调用方每项必须指定当前存在的库，并提交可读 Markdown。链接、作者、发布者、发布时间和获取方式可选；未知留空，提供时校验格式。草稿、片段和摘要可以收录，空内容、纯链接、仅图片及无效图片会报错。资料入库后不能换库，旧审核标准和来源配置不作为新收录门槛。选库与字段说明见 [外部接入总览](external-integration.md)。

资料先保存正文、图片和元数据，再后台建立正文索引，随后运行摘要、分类、主题、实体和图谱。摘要包括一句话概述、原文支持的实际要点与关键名词，不固定要点数量。图谱默认抽取有依据的关系，可显式设 `graph:false`；实体和主题按库独立。模型失败只影响派生结果和重试，不撤销已保存正文和已建立的全文索引，也不转入常规待审。

同库只按 `source_ref`、`identifier` 或规范化 URL 精确定位来源。唯一匹配且保存内容未变时返回原 ID；有变化覆盖原记录，不保留旧正文版本。无可靠来源标识时新建，不凭相同或近似正文自动归并，不新增转载归并记录。历史人工拒收记录继续生效，历史转载来源仍可读取。相似报道折叠只影响检索与动态展示，不合并资料。

外部 AI 通过相同的 HTTP `/api/v1/ingest` 或 MCP `ingest` 写回，先选择实际目标库，设置 `acquisition: "model_written"`，将来源引用写在正文中。AI产出也执行正文索引和后台模型处理，保留 `cannot_be_fact=true`；成功处理不代表结论已核实。稳定的 `source_ref` 可用于重试和更新同一报告。

历史待审资料可逐条批准、补充链接后接收、确认无链接入库或丢弃。人工无链接入库保留来源未核实标记。所有操作继续记录日志。模型支持现有 OpenAI 兼容、Anthropic 与 Ollama 协议；各库配置处理、实体判断与重排模型，向量模型由空间共享。

## 文件库与检索

Compose 将宿主机 `RELIQUARY_VAULT_DIR` 绑定到容器内的 `VAULT_ROOT`。默认文件库为 `~/Reliquary/vault`，上传资源为 `~/Reliquary/files`，备份为 `~/Backups/Reliquary`。三者均可在 `.env` 修改为其他绝对路径。文件库使用各库实际配置的英文目录。下表仅为历史目录示例，不表示系统会自动创建这些库：

| 目录 | 内容与使用方式 |
| --- | --- |
| `official/` | 官方信息的统一表头及完整正文 |
| `media/` | 媒体信息的统一表头及完整正文 |
| `references/` | 参考文档的统一表头及完整正文 |
| `personal/` | 个人产出的统一表头及完整正文 |
| `ai-output/` | AI产出的统一表头及完整正文 |
| `<自建库英文目录>/` | 自建库的统一表头及完整正文 |
| `attachments/<资料ID>/` | 仅图片，不按库分类 |
| `.reliquary/` | 数据库维护副本、持久表登记和配置 |

在 Obsidian 选择“打开文件夹作为仓库”，打开 `vault/` 这一层，例如 `~/Reliquary/vault`。建议在 Obsidian 设置的“文件与链接 > 排除的文件”中加入 `attachments/` 与 `.reliquary/`，避免搜索附件和维护副本。正文图片使用相对路径指向 `attachments/<资料ID>/`，可直接显示。

所有库的正文在保存后保持原样，只通过显式正文更新接口替换。显式导出时按 `<库目录>/<标题短名>-<ID前8位>.md` 生成文件，不设年月子目录。不要在 Obsidian 修改正文、表头或 `.reliquary/`；手放文件不会收录，手改表头不会回写数据库，刷新及重建会覆盖。星标、收录理由、内容类型、实体与主题以数据库为准。同库同一 `source_ref` 可提交新正文，变化覆盖原记录，不保留旧正文版本。上传过程使用 `LOCAL_STORAGE_BASE_DIR`，备份同时包含上传目录和文件库。统一表头的字段顺序见[维护数据](maintenance-data.md#按库文件与统一表头)。

Obsidian 的 `.obsidian/` 阅读配置、`.trash/` 和 `.DS_Store` 等用户配置不会参与文件库核对、重建或索引，程序不会修改或删除它们。备份和迁移包包含这些文件，保留阅读配置；恢复核对不比较其内容。

日常运行不再安排 Markdown 表头或恢复副本刷新；旧配置 `VAULT_HEADER_DEBOUNCE` 不再影响日常存储。资料正文不监听文件变化，也没有索引等待时间设置。资料导出与备份页展示全部库的名称、子目录与已归档资料数，包括空库和自建库。数量按数据库中的归档记录统计，不代表已核验磁盘文件完整性。

普通检索必须选择一个或多个库，使用 PostgreSQL BM25 与向量混合检索加可选重排，按资料合并命中并返回正文位置。可按内容类型、实体、主题、发布时间与星标筛选；时间筛选排除缺日期的资料，不用收录时间替代。图谱检索按库查询实体的 1 至 3 跳关系或两实体间路径，不调用模型，返回 PG 来源、媒体信息发言人、位置和待同步数量。详见 [普通与图谱检索](search-api.md)。

只运行一个后端进程。模型并发、速率和接口限流由该进程维护；待处理操作与业务数据在同一 PostgreSQL 事务中持久化，Redis（asynq）只负责调度唤醒，设置保存后立即刷新进程内缓存。数据库备份同时包含未完成任务，恢复后任务可继续执行。文件库与数据库仍需一致性备份，迁移前应停止写入并等待运行队列完成。详见 [ADR 0014](adr/0014-internal-knowledge-base.md)。

## 使用 ChatGPT 订阅

Reliquary 可以通过自己的 OpenID Connect 登录，使用符合条件的 ChatGPT Plus 或 Pro 账号完成文章处理、主题历史归入和实体判断。此方式适用于开源、本地部署应用，具体模型和权限以账号授权结果为准。它不会读取 ChatGPT 对话，也不会读取 Codex 的本地凭据。Embedding 和 rerank 继续配置其他模型来源。参考 [官方快速开始](https://developers.openai.com/siwc/quickstart) 和 [开源应用说明](https://developers.openai.com/siwc/token-sharing-open-source)。

1. 在 `.env` 设置恰好 32 字节的 `SYSTEM_AES_KEY`。访问令牌、刷新令牌和保留的 ID token 都以 AES-256-GCM 加密后存入 PostgreSQL，没有明文降级。维护命令不把这个密钥或数据库中的加密凭据写入备份或迁移包。恢复时生成新密钥，按清单重新授权 ChatGPT 和填写其他凭据。
2. 配置 `CHATGPT_CALLBACK_URL=http://127.0.0.1:8080/auth/callback`，其中 `8080` 应与 Docker Compose 的 `APP_PORT` 后端映射端口一致。回调通过宿主机浏览器访问映射端口进入容器中已启动的后端；容器监听地址无需改成回环地址。回调必须使用 `127.0.0.1`，不能替换为 `localhost`，路径固定 `/auth/callback`。浏览器应运行在这台 Mac mini 上；其他电脑的 `127.0.0.1` 指向它自己，如需跨电脑操作，先建立访问该后端端口的本地 SSH 隧道。`CHATGPT_SETTINGS_URL` 设置成功页面返回模型设置的链接，默认前端地址为 `http://127.0.0.1/platform/settings?section=models`，前端端口变化时同步修改。参考 [官方登录流程](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)。
3. 在现有 SSRF 白名单配置中保留已用条目，并追加 `auth.openai.com,api.openai.com`。例如已有本地 Ollama 白名单时设置 `SSRF_WHITELIST=host.docker.internal,auth.openai.com,api.openai.com`。授权发现、JWKS、令牌刷新和撤销使用 `auth.openai.com`，模型目录和 Responses 使用 `api.openai.com`；所有请求沿用现有 SSRF 防护。
4. 重启后端，进入“设置 → 模型设置”，点击 **Continue with ChatGPT**。在新标签页完成账号、工作区和使用订阅额度的授权，确认回调页成功，再回到原设置页。首次使用会显示订阅用量确认。首次注册的客户端 ID 与部署主机 ID 都会保存，重启和再次登录继续使用；多个账号注册分别显示和保存。
5. 新建聊天模型，选择“用 ChatGPT 登录”，选择已登录的账号和从该账号目录加载的模型。显示名称来自服务端 `display_name`，调用使用 `slug`。将模型分配给生成、轻量审核或实体判断层级。选中支持图片的模型时，可以启用现有视觉能力选项。参考 [模型与推理](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference) 和 [预览限制](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)。

设置中可配置每日调用次数上限，留空表示不限。此计数统计 Reliquary 的生成调用尝试，失败也会计入；每天 UTC 00:00 清零，北京时间为 08:00，不是 OpenAI 的 token 余额。达到上限后暂停，次日自动恢复，也可提高或移除上限。手动恢复不会清零今日次数。

OpenAI 返回 `subscription_sharing_usage_limit_exceeded` 时，设置与运行队列显示额度暂停，并提供 **Manage usage** 链接到 [ChatGPT 用量设置](https://chatgpt.com/settings/usage)。该错误可能对应应用上限，不能仅凭错误码推断全部订阅耗尽或具体重置时间；调整额度或等待官方用量页显示恢复后，点击“手动恢复”。`subscription_sharing_usage_unavailable` 表示暂时无法确认用量，保留凭据并在五分钟后再试。暂停期间未完成的任务延后执行，额度暂停不消耗普通失败重试次数，文章的原始内容保留，主题批次保持待处理。参考 [错误与恢复](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery)。

修改资料内容类型需按所属库校验并联动图谱。需要同步调用生成模型时，生成暂停返回可重试的 HTTP 503 与 `Retry-After`；操作失败保留原资料和分类，恢复后需再次发起。

手动恢复解除生成暂停，已经延后的任务会在原定重试时间运行；需要立即重试文章时，在运行队列对相应任务点击“立即运行”。主题历史归入由后台轮询按批次恢复。

可明确选择一个已启用的本地 Ollama 聊天模型作为额度不足时的备用。配置后，暂停期间的调用使用该备用模型，包括触发额度错误的当前调用；没有备用时只暂停。备用模型同样需要具备任务所需的文本或图片能力。服务器不会自动改用其他付费 API。已发出的并发调用可能在暂停被记录前完成。

与 Codex 开发共用同一 ChatGPT 账号时，需要一起留意订阅用量。官方说明 Plus 的五小时额度在使用订阅的各应用之间共享，Pro 不适用这项五小时上限；各模型、应用及账号权限仍以用量页为准。Reliquary 的每日调用次数限制只限制本应用，不能显示或控制 Codex 的用量。参考 [账号、会话与用量](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)。

访问令牌接近过期时自动刷新并原子保存轮换后的凭据。刷新失败会显示“需要重新登录”；临时网络或服务器失败保留加密凭据并允许之后重试，不可用的刷新令牌会清除。点击对应账号的 **Continue with ChatGPT** 重新授权，模型仍关联该账号注册。参考 [令牌说明](https://developers.openai.com/siwc/token-sharing-open-source/token-reference)。

退出时，在模型设置中点击该账号的“退出登录”。后端先尝试撤销可续期会话，再清除本地访问、刷新及 ID token，保留账号与客户端映射、主机 ID。若未确认远程撤销，界面会提示到 ChatGPT 设置断开应用。退出后该账号不再发起生成请求，重新登录后可继续使用原模型配置。

## 关注与简报

在实体页、主题页，或文章详情的实体与主题标签旁点击“关注”，再次点击即可取消。关注与取消关注会写入操作日志。文章星标继续作为书签，可独立使用。

侧栏“关注”位于主题之后，显示未看新文章数。打开关注动态可集中查看与任一关注对象关联的已完成文章，按收录时间倒序，每篇显示命中的实体与主题；可以按对象筛选，也可以开关相似报道折叠并展开报道。用户文档、整理结果、收录待审内容不出现在动态中。

“新”根据进入动态时间判定：取文章处理完成且满足动态资格的时间与实体、主题关联建立时间的较晚者，多个命中取最新时间。晚于上次成功打开关注动态的文章标为新，因此查看时仍在处理的文章，完成后仍能显示为新。新增关注不重置旧文章的进入时间。主题历史归入单独标记为历史关联，不产生新动态，只在历史列表展示；首次打开前，其余符合条件的文章视为新。首次加载成功后记录本次查看时间，本次显示的新标识仍保留，之后进入动态的文章继续计入侧栏数字。“全部标为已看”更新查看时间并清除当前新标识。数字按文章去重，与相似报道是否折叠无关。实体合并时关注转移到保留实体并去重，保留最早关注时间；删除主题会删除该主题的关注。

Reliquary 提供资料，不生成周报或推送通知。下面是外部 AI 通过已配置 MCP 编写周报的示例流程。完整技能包与使用规则见 [Reliquary skill](../skills/reliquary/SKILL.md)，多步任务可先调用 `reliquary_guide`：

1. 调用 `list_follows`，读取关注对象的 `target_type`、`target_id`、名称、关注时间和近 7 天资料数，确认本周关注方向。
2. 调用 `follow_feed`，显式指定周报开始时间，例如 `{"since":"2026-10-05T00:00:00+08:00","limit":100,"collapse_similar":true}`。只整理某些对象时，增加 `target_ids`。省略 `since` 使用网页上次查看时间；MCP 调用不会更新该时间。since 和排序均使用资料进入动态的时间，逐库检查点控制新动态。每次收到 `next_cursor`，继续调用 `follow_feed`，传入 `{"cursor":"上一页的 next_cursor","limit":100}`，直到响应没有 `next_cursor`；再开始整理全部主项及折叠成员。
3. 根据返回的 `results`、`matched_follows` 和 `similar_reports` 归纳线索。需核实的全文资料用 `get_entry` 分段读取。资料 ID 用于引用和去重，勿把相似报道当作独立事件重复统计。
4. 由外部 AI 写出 Markdown 周报，注明时间范围、来源资料与资料限制，再调用 MCP `ingest` 保存到 AI产出，例如：

   ```json
   {
     "items": [{
       "library": "ai-output",
       "title": "关注周报 2026-10-05 至 2026-10-11",
       "content": "# 关注周报\n\n## 产品进展\n\n根据本周原文归纳进展，并逐条注明来源资料链接。\n",
       "acquisition": "model_written"
     }]
   }
   ```

`follow_feed` 默认返回最多 20 个主项，`limit` 最大为 100；相似报道作为主项的 `similar_reports` 返回，不占主项额度。`total` 是首请求窗口内折叠前的候选资料数。游标保留首请求的时间上限、since、对象筛选和折叠开关，后续可只传 cursor 与 limit。必须翻页直到没有 next_cursor，再按资料 ID 汇总主项和折叠成员。AI产出写入 `ai-output/` 后可检索，仍不会进入关注动态。

## 部署在 Mac mini

长期运行建议使用[源码与运行分离的本地部署](local-deployment.md)。下文保留直接从源码目录部署的方式。

Mac mini 使用 Apple Silicon，安装 Git、Node.js/npm 和支持 Docker Compose 的 Docker 兼容运行环境（如 OrbStack、Docker Desktop）。四个核心服务为应用、PostgreSQL（ParadeDB）、Redis 与 Neo4j，均使用 Compose；网页静态资源由独立 frontend 容器代理，模型服务可在 macOS 原生或其他端点运行。应用镜像从当前仓库构建，未发布公共应用镜像。

```bash
git clone https://github.com/Dikui/Reliquary.git
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
| `SYSTEM_AES_KEY` | 设置自己的 32 字节凭据加密密钥；迁移和整体恢复时由维护命令生成新值 |
| `OLLAMA_BASE_URL` | 容器访问 macOS 模型服务的地址，例如 `http://host.docker.internal:11434` |
| `OLLAMA_OPTIONAL` | 设为 `true` 时，Ollama 暂不可用只输出告警 |
| `SSRF_WHITELIST_EXTRA` | 部署额外放行的模型服务主机/IP/CIDR；与设置页白名单合并 |
| `NEO4J_URI`、`NEO4J_USERNAME`、`NEO4J_PASSWORD` | 使用 `bolt://neo4j:7687`、`neo4j` 与独立密码，须与 Neo4j 初始化一致 |
| `REDIS_ADDR` | Compose 内使用 `redis:6379` |
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
docker compose up -d --wait postgres redis neo4j
./reliquary-admin check-deployment
docker compose up -d --no-build
```

`./scripts/start-all.sh --docker` 也会在应用启动前创建并检查宿主机目录，先启动数据库，再核对已保存的备份设置。应用入口随后为挂载目录设置容器用户写入权限，并以 `appuser` 运行。在 macOS 的 Docker 兼容运行环境中，宿主机登录用户仍可读取文件库供 Obsidian 使用；无需手工执行递归 `chown`。运行环境的文件共享设置应允许访问所选目录。

启动后在本机访问 `.env` 配置的网页端口，配置共享向量与各库模型并收录一份资料。检查宿主机文件库是否产生 Markdown 和附件，知识检索是否命中。后续执行 Compose、备份或恢复时，使用相同的 `COMPOSE_FILE` 和 `COMPOSE_PROJECT_NAME`。数据库仍使用原有命名卷；不迁移旧的文件库或上传资源命名卷。

推荐将 Ollama 直接安装到 macOS，在宿主机运行生成和嵌入模型。Ollama 支持 Apple GPU 的 Metal 加速；其官方文档说明 macOS 上的 Docker 容器不能使用 GPU 加速。因此将模型服务放在 macOS，Compose 中只运行 Reliquary 与数据服务，不依赖 CUDA。参见 [Ollama 硬件支持](https://docs.ollama.com/gpu)与 [Ollama FAQ](https://docs.ollama.com/faq#how-do-i-use-ollama-with-gpu-acceleration-in-docker)。

若 Ollama 桌面应用仅监听回环地址，可按[官方环境变量说明](https://docs.ollama.com/faq#setting-environment-variables-on-mac)执行 `launchctl setenv OLLAMA_HOST "0.0.0.0:11434"` 并重启 Ollama，让 Docker 通过 `host.docker.internal:11434` 访问。使用命令行服务时可在单独终端执行 `OLLAMA_HOST=0.0.0.0:11434 ollama serve`。让此端口只在可信内网可达。选择能放入统一内存的模型，先测试文章处理与嵌入，再调整后台并发。

若模型连接被 SSRF 校验拒绝，可把实际使用的 `host.docker.internal` 或模型主机加入 `SSRF_WHITELIST_EXTRA` 后重启应用。Compose 默认额外放行 `198.18.0.0/15`，用于本机代理 fake-ip；没有此需求可在 `.env` 显式设置 `SSRF_WHITELIST_EXTRA=`。默认网段本身不会放行回环、私网或 Docker 服务名，额外配置应写明具体需要访问的主机。

### 按库重新处理资料

在资料列表筛选目标库，选择全部匹配资料，再进入批量管理并选择重新处理。只处理失败资料时叠加失败状态筛选。预览会显示可执行范围与影响，确认后固定名单并排队；新入库资料不加入本批次。AI产出也支持重新处理。

重新处理使用当前库配置，重建正文索引、摘要、分类、实体和关系。正文、图片、资料 ID、来源及已有人工判断保留；结果在运行队列和资料批次中查看。后台处理失败不改变为内容待审，可按原任务重试。接口与确认流程见 [资料批量管理](batch-operations.md)。

### 历史待审资料

整库与批量重新审核入口已移除。历史待审资料仍可逐条批准、补充链接、确认无链接入库或丢弃，按当前规则保存和处理。丢弃会删除资料及独占来源材料，需确认影响；操作日志和人工拒收记录保留。旧审核批次的结果、已排队任务和失败重试仍可使用。

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

第一条输出逐项差异，成功后应没有未解决的差异。第二条根据数据库补回各库资料文件和数据库副本，再核对。也可运行 `./reliquary-admin maintenance rebuild-files` 重建全部库的 Markdown；图片字节不可重算，保留 attachments/ 或从完整备份恢复。错误时保留原库和故障文件，先检查输出指向的路径、权限和磁盘空间，再从完整备份恢复。不要通过删除数据库行消除差异。

## 四服务备份、恢复与演练

维护时使用部署目录的相同 `.env`、`COMPOSE_FILE` 和 `COMPOSE_PROJECT_NAME`。四个数据相关服务为 PostgreSQL、Redis、Neo4j、Reliquary；本机 Ollama 和重排服务分别提供向量与重排，不把模型权重放入数据备份。宿主机需要 Python 3、Git、Docker 与 Compose。先启动并检查 PostgreSQL、Redis、Neo4j，再启动应用：

```bash
./reliquary-admin check-deployment
docker compose up -d --wait postgres redis neo4j
docker compose up -d app
./reliquary-admin setup-models
```

新完整备份包含脱敏 PostgreSQL dump、原始 `attachments/`、上传资源及阅读配置，不包含生成的库目录 Markdown 和恢复副本；`.reliquary/exports/` 中的历史迁移资料仍保留。正文完整性由数据库正文哈希单独核验；历史完整备份仍按原布局恢复。关系正本、来源、测评题、答案、评测运行、库配置、检索记录和调用统计都由 PG 备份保留；检索记录与调用统计不进入文件副本。Redis 只保存任务唤醒，Neo4j 是可重建图谱，两者不备份。账号密码哈希保留，模型 API Key、收录 API Key、ChatGPT 令牌和系统密钥不进入备份，恢复时重新配置。

备份目录默认 `~/Backups/Reliquary`，也可在 `.env` 配置 `BACKUP_DIR`。目录应位于源码之外，与文件库和上传目录互不包含。备份时暂停收录与任务消费，停止应用，将 PG 与文件取成一致快照；长任务的检查点和待处理操作保留。成功核对后启动原应用并解除暂停。外部编辑器写文件也应暂停。

```bash
./reliquary-admin backup /Users/yourname/Backups/Reliquary --timeout 300
```

成功输出 `backup complete and read-back verified` 与具体备份路径。未变化文件用硬链接复用，每份成功备份都可独立恢复，默认保留最近四份。需要同时保存部署镜像和源码时加 `--with-images`，再用 `unpack` 恢复。失败或超时不会产生假成功备份。

完整恢复使用新的空 PG 数据库和空文件目录，目标应用保持停止。先填写目标 `.env` 中的四服务连接、`RELIQUARY_VAULT_DIR`、`RELIQUARY_FILES_DIR` 和新系统密钥。恢复不覆盖现有数据库或非空目录：

```bash
docker compose up -d --wait postgres redis neo4j
./reliquary-admin restore /Users/yourname/Backups/Reliquary/backup-具体时间 --skip-models
```

命令恢复 PG 与文件库，强制 snapshot 暂停，重建全文索引并按原备份清单核对。离线 `queue-replay` 将中断操作恢复并把所有到期唤醒推入 Redis，未来操作保留 process_at。Redis 完全为空也可以恢复。随后启动应用供配置；消费者每次领取仍检查 PG 暂停门禁，不会提前执行任务或消耗重试预算。

按清单填写模型凭据、重新登录 ChatGPT，重新生成收录 API Key。确认本机 Ollama 及重排服务地址仍可达，向量模型与维度符合原配置，然后继续：

```bash
./reliquary-admin setup-models
./reliquary-admin unpack --resume
./reliquary-admin verify --vectors
./reliquary-admin maintenance graph-verify
```

继续命令核对原件与不可再生数据，从 PG 全量重建 Neo4j，核对实体、边及来源计数，刷新新增系统重建日志后的维护副本，最终核对通过才解除暂停。向量模型发生变化时先重建全部向量。缺模型、缺凭据、图谱不可用或任何核对差异都会保留暂停。`State: ready` 后后台补推并完成未完成操作；`--resume` 继续原恢复，不再次导入。独立验收环境可对 `unpack --resume` 加 `--no-scheduler`，避免注册本机自动计划。

一键演练从指定目录选择最近一份完整备份，不需要关闭正常部署：

```bash
./reliquary-admin drill /Users/yourname/Backups/Reliquary \
  --report /Users/yourname/Backups/Reliquary/drill-result.json
```

演练在临时 PG 数据库、独立 Redis 容器中的临时 DB 0、临时 Neo4j 命名空间和临时目录恢复并核对。它重推真实待处理操作的唤醒，独立探针验证暂停期间零执行、解除暂停后一次完成；业务队列保持暂停，演练不会调用模型或外部工具。报告含原件核对、补推数量、暂停探针、图谱重建和最终核对结果，`status: success` 才表示通过。结束清理临时数据库、Redis、图谱空间和目录，不清空线上 Redis 或 Neo4j。

失败也清理自己创建的资源，清理失败时保留 `.maintenance-schedule/drill-active.json` 记录，执行 `schedule run --once` 重试清理，错误及报告保留。普通命令把报告写到 `.maintenance-schedule/drill-result.json`；`--report` 可指定永久存证位置。

每月备份、每月演练可以安装宿主机计划，macOS 用 launchd，Linux 用用户 systemd。下面配置北京时间每月 1 日 03:00 备份、13:00 演练：

```bash
./reliquary-admin schedule configure \
  --backup-directory /Users/yourname/Backups/Reliquary \
  --backup-schedule '0 3 1 * *' --drill-schedule '0 13 1 * *' \
  --timezone Asia/Shanghai --enabled true
./reliquary-admin schedule install
./reliquary-admin schedule run --once
```

Docker 必须保持可用。调度器每分钟检查，主机休眠错过计划后补跑；日志和中断记录位于 `.maintenance-schedule/`。换目录或机器后重新安装。关闭计划执行 `schedule configure --enabled false`。

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

接下来必须看到核对结果和 `Reconfiguration checklist`。缺少本地模型时状态为 `waiting_models`，等待模型就绪；已选择的模型缺少凭据时为 `waiting_credentials`，等待重新填写。两种状态都会启动网页供修改设置，但暂停收录与领取处理任务，保留所有中断任务。命令因等待返回非零状态是预期行为；不要通过手工 `maintenance resume` 跳过检查，也不要重复执行初次 `unpack` 覆盖已恢复的数据库。

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

命令读取表头、正文和 `.reliquary/`，恢复资料及表头未保存的用户数据、档案、待审内容、日志、配置和任务检查点，再生成全文索引并逐表核对原副本。`.obsidian/`、`.trash/` 和 `.DS_Store` 等用户配置保留原样，不作为资料索引。导入前先确认 PG 为空并进入 snapshot 暂停，队列不会在导入或配置期间领取任务或消耗重试预算。缺少或损坏的副本、文章、待审附件会列出错误并停止。库目录中没有有效表头或未在副本登记的文件不会作为已有资料导入，不兼容旧的按来源类别目录规则。可重建的索引和向量不作为唯一原件。

随后可以执行 `docker compose up -d app` 启动应用，用原账号密码登录。按照程序输出的清单重新填写模型凭据，ChatGPT 重新登录；账号密码哈希保留。此时处理队列与收录仍暂停，不要手工解除暂停。完成后执行：

```bash
./reliquary-admin setup-models
./reliquary-admin rebuild /Users/yourname/RecoveredVault --resume
```

第一条确认配置模型可用；第二条只继续已经重建的库，读取初次保存的固定核对清单，不再次导入数据。它先严格核对，再检查模型及必需凭据，自动补齐全部向量，保留实体档案时间，从关系正本全量重建 Neo4j 并核对，核对向量完整性并安装本机备份调度。缺模型、缺凭据、向量失败、核对不一致或调度安装失败时继续暂停并返回非零状态。全部通过才显示 `State: ready`、解除维护暂停并让原有任务自动继续。其他未配置功能仍暂停。隔离演练可加 `--no-scheduler`，日常使用默认安装。初次清单保存在部署目录的 `.maintenance-rebuild.json` 等维护状态文件中，位于文件库之外，不进入迁移包。不要删除清单或重新执行初次 rebuild 来跳过失败。

没有 `.reliquary/` 时，`vault-rebuild` 只能从当前按库目录下的统一表头和正文恢复部分资料，无法凭空恢复档案说明、日志、关注、待审与库配置。旧 `articles/`、`reports/` 布局不受支持。应保留原件，寻找完整数据库备份，不要宣称完整重建成功。

任何库的 Markdown 文件损坏时，数据库保存的完整正文与统一表头可以恢复资料文件，包括 AI产出。图片仍需完整文件库备份保护；整个磁盘丢失时先恢复图片和维护副本，再重建资料文件。

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

升级默认不备份，也不生成升级前全量清单。命令准备镜像后暂停收录、停止应用，执行必要迁移并核验当前数据一致性，通过后恢复使用；默认不做升级前后全库摘要比对。需要本次升级前的恢复点时，明确运行 `./reliquary-admin upgrade --backup`，该模式保存旧代码、旧镜像和旧数据，并按备份清单核对。月度备份独立执行，不与部署绑定。

执行 `000005` 前仍先将 `data_sources` 与 `sync_logs` 的非密钥内容导出到文件库 `.reliquary/exports/`，JSON 包含导出时间与迁移版本。导出失败则不执行迁移并保持暂停。版本 4 及以前的完整备份仍包含这两表；旧备份恢复到当前版本时也先导出再迁移，已有文章、附件及历史渠道不删除。开发环境若直接运行迁移，必须先执行 `./reliquary-admin export-sources`。失败时不解除维护暂停；没有本次升级前备份时，恢复只能使用另外保留的恢复点。不要运行迁移 `down` 或修改 `schema_migrations` 来退回生产。

退回需要用已有快照恢复旧代码、旧镜像、旧数据库和旧文件库到新的空环境。`upgrade --backup` 或手动 `backup --with-images` 产生的完整备份包含这些镜像，可在该备份目录执行随包提供的入口：

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

Grok Bot、Claude Code 等外部工具需与 Reliquary 运行在同一台电脑上，端口只绑定 `127.0.0.1`。打开网页，在“设置 > API 集成”为每个工具创建自己的 API Key，复制完整令牌和连接地址。完整令牌只通过创建响应的 `token` 返回一次，页面关闭创建弹窗后仅显示掩码。同页可查看 Key 名称、修改名称与有效期或撤销 Key；创建、列表和编辑响应均不含存储用的 `api_key`。

将 [外部接入总览](external-integration.md) 与 [收录接口说明](ingest-api.md) 提供给外部工具开发者或 Agent。该文档包含 Markdown 与图片要求、认证、查重与收录顺序、可执行示例、HTTP 与 MCP 接口、补全、重试和用户反馈。外部工具负责取得原始资料，Reliquary 负责去重、模型审核、处理、存储与检索。收录结果和待处理页面供用户确认后续操作。

## Redis 任务调度

`REDIS_ADDR` 默认 `127.0.0.1:6379`，Compose 内默认使用 `redis:6379`。`REDIS_DB` 默认 0，`REDIS_PASSWORD` 可用于连接外部 Redis。内置 Redis 仅开放本机端口，不启用持久化。PG 是任务状态、重试预算、配额延后和死信的权威；备份不需要 Redis 数据。Redis 清空或恢复备份后，后端启动会补推所有未完成操作，运行中每 `RELIQUARY_QUEUE_SWEEP_INTERVAL`（默认 1 分钟）补推；投递超过 `RELIQUARY_QUEUE_DELIVERY_TIMEOUT`（默认 2 分钟）会换投递代次重推。保留一个后端进程。

## 本机文本向量与重排

默认文本向量模型是 Ollama 官方库的 `qwen3-embedding:0.6b`，维度 1024。空间共享一个当前向量模型，已有明确选择保持原值。安装、下载和启动示例：

```bash
brew install ollama
ollama serve
# 在另一个终端下载模型，下载完成后请求会按需加载。
ollama pull qwen3-embedding:0.6b
```

模型名依据 [Ollama 官方模型页](https://ollama.com/library/qwen3-embedding)。后端连接宿主机 `OLLAMA_BASE_URL=http://127.0.0.1:11434`；Docker 内改为 `http://host.docker.internal:11434`。无服务或模型未安装时仍可启动，收录的正文和全文索引先完成，向量待处理操作延后重试。程序不自动下载模型。Ollama 使用默认空闲卸载策略，首次请求可能包含加载时间；查询向量超过五秒时本次退回全文检索。不要用 `:latest` 代替 `:0.6b`，前者不是 0.6B 型号。

Ollama 不提供本任务所需的重排接口。建议独立运行 llama.cpp server 配合 bge-reranker-v2-m3 GGUF；Qwen3-Reranker-0.6B GGUF 可作为支持该架构的 llama.cpp 版本的替代模型，需自行测评。代码只约定协议，不绑定模型名称。

```bash
brew install cmake git
git clone https://github.com/ggml-org/llama.cpp.git
cd llama.cpp
cmake -B build -DCMAKE_BUILD_TYPE=Release -DGGML_METAL=ON
cmake --build build --config Release -j --target llama-server
mkdir -p "$HOME/models"
curl --fail --location \
  https://huggingface.co/gpustack/bge-reranker-v2-m3-GGUF/resolve/main/bge-reranker-v2-m3-Q8_0.gguf \
  --output "$HOME/models/bge-reranker-v2-m3.gguf"
./build/bin/llama-server -m "$HOME/models/bge-reranker-v2-m3.gguf" \
  --host 127.0.0.1 --port 18802 --embedding --pooling rank
```

GGUF 示例来自 [GPUStack 发布的量化模型](https://huggingface.co/gpustack/bge-reranker-v2-m3-GGUF/tree/main)。启动参数和协议以 [llama.cpp server 文档](https://github.com/ggml-org/llama.cpp/tree/master/tools/server) 为准。在模型设置中新建 `Rerank` 类型，来源使用通用 HTTP，`base_url=http://127.0.0.1:18802/v1`，填写实际模型名称，并选择重排模型。容器访问使用 `host.docker.internal`；仅在现有 `SSRF_WHITELIST` 中追加相应主机名或 `127.0.0.1`。无需填写虚构 API Key。

客户端发送 `POST /v1/rerank`，JSON 为 `model`, `query`, `documents`, `top_n`，接收 `results` 或 `data` 中的 `index` 与 `relevance_score`，兼容 Jina/Cohere 及 llama.cpp 格式。超时或错误保留原排序并返回 `rerank_status=unavailable`；未选择模型返回 `unconfigured`。接口字段、正文位置与两套向量切换参见 [普通检索接口](search-api.md)。
## 长文配置

`config/config.yaml` 中的 `article_processing` 控制后台内容处理：

```yaml
article_processing:
  max_body_chars: 0
  max_images: 8
  max_completion_tokens: 0
  max_segments: 128
  segment_overlap_chars: 128
  context_reserve_percent: 40
```

| 配置 | 含义 |
| --- | --- |
| `max_body_chars` | 每段正文的码点上限。0 按所选模型上下文计算；模型未声明上下文时回退到 24,000。正值只降低预算，不能突破模型上下文。 |
| `max_images` | 每段最多输入的图片数，默认 8。 |
| `max_completion_tokens` | 每次模型调用的输出预算，0 默认最多 4,096，并受模型上下文与输出上限约束。 |
| `max_segments` | 单篇后台处理的最大段数，0 使用 128。超过时记录处理失败，保留正文与已建立的索引。 |
| `segment_overlap_chars` | 超长单块切分时附带的前文码点数，0 使用 128。前文单独标注，不改变每段负责的正文范围。 |
| `context_reserve_percent` | 上下文安全余量百分比，0 使用 40，可设 1 至 90。还会扣除提示词、资料和主题元数据、图片、输出预算及分段上下文预留，采用较小预算。 |

预算按一个正文码点约占一个 token 作保守估计。请为库的处理模型填写实际 `context_window`，程序据此计算处理预算；修改配置后重启服务。所有分段调用继续使用现有模型并发限制，每篇内部顺序执行。

长文处理失败后保留原文和逐段检查点，再次重试可续跑。正文、模型输入配置或分段规则变化会重新处理全文。合并摘要过大时会明确报错，可选用更大上下文模型或将资料拆成多篇，不能靠提高 `max_body_chars` 越过模型上限。备份及文件库副本包含检查点，恢复后可继续未完成的处理。

## Neo4j 图谱查询副本

部署前设置 `NEO4J_URI=bolt://neo4j:7687`、`NEO4J_USERNAME=neo4j` 和独立的 `NEO4J_PASSWORD`。初始管理员用户名固定为 `neo4j`，修改密码后需要同时更新 Neo4j 和后端配置。Compose 固定 Community 5.26.12 与镜像摘要，有认证和专用数据卷，默认不对外暴露 Bolt 端口。32 GB 小型服务器使用 512 MB 初始堆、1 GB 最大堆、1 GB 页缓存和 3 GB 容器内存上限。

PostgreSQL 是唯一正本。业务修改和图谱通知在同一 PG 事务提交，后台通过 PG 待处理操作和 Redis 唤醒同步。默认启动后立即补推，此后每分钟扫描；`RELIQUARY_GRAPH_SWEEP_INTERVAL` 可调整扫描周期。Redis 丢失后仍可从 PG 补推。Neo4j 不可用时后端照常启动和收录，通知保留，运行队列可见重试及错误；副本恢复后自动重读 PG 当前关系。

每天执行一次全量排序 SHA-256 核对，同时比较实体信息、边数、关系类型、库、来源数量与版本。发现偏差时以 PG 为准修复，结果写入操作日志 `graph_check`。图快照占用 O(V+E) 内存，PG 来源计数还需读取 S 条来源行，读取代价为 O(V+E+S)，核对与重建期间会暂时锁定 PG 关系修改，适用于当前小型部署。同步也持有短时关系锁，离线检测先于该锁，任务超时为 15 秒。

手工重建命令为 `./reliquary-admin maintenance graph-rebuild`，手工核对为 `./reliquary-admin maintenance graph-check`。网页用户的 `POST /api/v1/maintenance/graph/rebuild` 将重建排入运行队列，返回操作 ID。重建清空专用 Neo4j 数据库，在一个 Neo4j 事务内从当前 PG 全量创建实体与关系并核对哈希；不能把这个数据库与其他应用共用。

备份不包含 Neo4j 卷和 Redis。`reliquary-admin` 恢复当前版本备份时，在解除维护暂停前自动重建 Neo4j。Neo4j 不可用或重建失败时恢复不会解除暂停，修复连接后再次使用恢复的 `--resume`。恢复到新环境会生成新 Neo4j 密码，目标环境显式提供的 Neo4j 地址、账号、密码和端口优先。历史版本缺少图谱同步结构时保持原版本恢复流程，升级后由每日核对生成副本。

来源资料、发言人和位置明细只保存在 PG，Neo4j 只含全局实体节点及关系概要。HTTP 与 MCP 图谱检索已提供，见 [图谱检索](search-api.md#图谱检索)。

资料契约使用 `entry_id`、`entry_ids`、`latest_entry_id`、`related_entry_ids` 和 MCP `get_entry`。结构版本 23 与副本格式 6 的改名和历史恢复规则见 [迁移规范](database-migrations.md#资料对外命名迁移)。
