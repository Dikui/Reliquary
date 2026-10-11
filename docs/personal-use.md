# 个人使用指南

Reliquary 在一个个人空间的唯一知识库中收集、存储、去重与检索文章。综合分析和写报告由外部 AI 通过收录接口与 MCP 完成。统一术语和设计见 [CONTEXT.md](../CONTEXT.md) 与 [ADR 0009](adr/0009-kb-collect-store-retrieve.md)。

## 账号与网页入口

系统只有一个用户。首个用户注册成功后关闭注册，登录用户与有效 API Key 可以访问个人空间全部内容。API Key 支持名称、有效期和撤销，使用 `Authorization: Bearer <key>` 鉴权，现有 REST 接口也接受 `X-API-Key`。

登录后进入文章页。主要入口为文章、知识检索、实体、主题、关注和设置。文章页的待审区分为收录待审与处理待复核，可以处理收录审核、确认或修正模型类型，标记星标，修正内容类型和主题，并简单查看文章。实体页管理规范名、别名和合并；主题页管理名称、说明及历史文章归入进度。设置集中提供知识库、订阅源、模型、文件库状态、操作日志、API 集成和运行队列。

用户可在页面修改密码。忘记密码时，保持数据库可用，在仓库根目录使用服务器相同的数据库配置执行：

```bash
go run ./cmd/server reset-password
```

新密码从标准输入读取，最少 8 个字符，UTF-8 编码不超过 72 字节。修改会撤销旧登录令牌。Docker 部署可执行 `docker compose run --rm --no-deps app ./Reliquary reset-password`。

## 收集、审核与处理

收集入口包括全局 RSS 订阅、文件上传、按网址收录以及 `POST /api/v1/ingest` 收录接口。订阅源可设置默认内容类型，保留计划同步、手动同步和同步日志。RSS、上传、手动录入与收录接口一律经过模型收录审核，未通过的内容进入收录待审，只在数据库保留，审核通过后才写入文件库。调用方自身的判断不会替代模型审核。

规范化网址、正文哈希与近似重复检测用于去重，转载只补充来源记录。一篇文章通过一次处理调用得到内容类型、统一摘要、实体和主题。模型返回 content_type_uncertain=true 时，无论所选类型是什么都全文保存，正常入库并可检索，同时进入处理待复核，记录模型类型及原因。可确认模型类型、改为其他类型或忽略；落实摘要加链接类型前需再次确认删除正文。修正后的类型记为用户修正，所有操作记录日志。摘要包含一句话概述、3 到 5 条要点与关键名词。实体归一到实体档案，主题由用户定义，一篇文章可以属于多个主题。新建主题或修改说明后，可用历史摘要批量归入，页面显示进度。

| 内容类型 | 处理方式 |
| --- | --- |
| 事件、观点、研究 | 全文保存，正文、图片和原始文件写入文件库 |
| 教程、案例、参考 | 摘要加链接，需要时打开原链接阅读 |

处理模型支持视觉时，在同一次调用中读取正文图片。生成模型支持 OpenAI 兼容、Anthropic 原生与 Ollama 接口。嵌入支持 OpenAI 兼容与 Ollama；重排支持通用 `/rerank` 格式及阿里云百炼接口。在设置中测试模型连接、维度、视觉支持与实际调用，再配置知识库的处理和嵌入模型。

文本、Markdown 和图片使用 simple 解析；PDF、Office 与 EPUB 可使用进程内 anydoc，CSV 可使用 simple 或 anydoc。应用镜像默认以 `WITH_ANYDOC=1` 构建。直接 `go run` 时如需 anydoc，先执行 `make anydoc-lib`，再使用 `go run -tags anydoc ./cmd/server`；未链接时相关格式会提示构建要求。复杂排版与扫描件可选择已配置的 MinerU 或 MinerU Cloud 解析器。按网址收录使用 HTTP 抓取，需要 JavaScript 渲染的网页可能无法取得正文。

没有提供原始链接的上传或录入，先从提供的文件与正文提取 PDF 元数据 URL、DOI、arXiv 编号、HTML canonical 与 og:url，或“原文链接”“来源：”“Source:”等明确来源标注后的 URL，不联网搜索。提取链接标明“从文件提取”，随后按同一来源要求审核。没有找到时不调用审核模型，直接进入收录待审，原因为“缺少原始链接”。

缺链接的内容可补链接后重新审核，或确认无链接入库，或转为本人文档。无链接入库跳过模型审核，标记 source_unverified=true，始终全文保存，表头 url 为空；文章列表、详情与检索结果显示“来源未核实”，检索接口与 MCP 也返回该字段。转为本人文档先暂存解析正文，在事务中删除原收录记录并记录日志，再发布 mine/ 文档并按用户文档建索引；同名文件保留，采用带文章 ID 的文件名避免覆盖。

## 文件库与检索

`VAULT_ROOT` 指定服务器文件库，启动时创建以下目录：

| 目录 | 内容与使用方式 |
| --- | --- |
| `articles/年-月/` | 已批准文章的 Markdown 表头与正文 |
| `attachments/` | 全文文章的图片与原始文件 |
| `mine/` | 用户自己编辑的 Markdown 文档，程序只读并建立索引 |
| `reports/` | 外部 AI 写入的整理结果，可被检索 |

用 Obsidian 打开文件库即可阅读。正文首次写入后保持不变，表头由数据库单向生成；手动修改文章表头可能被下一次字段更新覆盖。星标、收藏理由、内容类型、实体与主题以数据库为准。上传和解析过程仍使用 `LOCAL_STORAGE_BASE_DIR` 保存通用资源，备份同时包含此目录。

`VAULT_HEADER_DEBOUNCE` 合并表头连续更新，默认 `5s`。`VAULT_STABLE_WAIT` 是用户文档停止变化后的索引等待时间，默认 `10m`；`VAULT_SCAN_INTERVAL` 是兜底扫描间隔，默认 `1m`。文件库状态页显示目录、最近扫描、文件数量与错误。

知识检索覆盖文章表头、全文正文、用户文档和整理结果，使用 PostgreSQL 全文与向量混合检索，按文章合并命中片段。可按内容类型、实体、主题、发布时间、星标及来源目录筛选。按发布时间筛选会排除缺少发布时间的文章并说明数量，采集时间不会代替发布时间。相似报道按摘要向量和时间窗口折叠，可以展开。

只运行一个后端进程。模型并发、速率和接口限流由该进程维护；Redis 保存 Asynq 后台队列及配置通知。迁移前先等运行队列完成，数据库备份不包含 Redis 中未完成的任务。

## 关注与简报

在实体页、主题页，或文章详情的实体与主题标签旁点击“关注”，再次点击即可取消。关注与取消关注会写入操作日志。文章星标继续作为书签，可独立使用。

侧栏“关注”位于主题之后，显示未看新文章数。打开关注动态可集中查看与任一关注对象关联的已完成文章，按收录时间倒序，每篇显示命中的实体与主题；可以按对象筛选，也可以开关相似报道折叠并展开报道。用户文档、整理结果、收录待审和处理待复核内容不出现在动态中。

“新”根据进入动态时间判定：取文章处理完成且满足动态资格的时间与实体、主题关联建立时间的较晚者，多个命中取最新时间。晚于上次成功打开关注动态的文章标为新，因此查看时仍在处理的文章，完成后仍能显示为新。新增关注不重置旧文章的进入时间。主题历史归入单独标记为历史关联，不产生新动态，只在历史列表展示；首次打开前，其余符合条件的文章视为新。首次加载成功后记录本次查看时间，本次显示的新标识仍保留，之后进入动态的文章继续计入侧栏数字。“全部标为已看”更新查看时间并清除当前新标识。数字按文章去重，与相似报道是否折叠无关。实体合并时关注转移到保留实体并去重，保留最早关注时间；删除主题会删除该主题的关注。

Reliquary 提供资料，不生成周报或推送通知。下面是外部 AI 通过已配置 MCP 编写周报的示例流程，只用于说明工具用法，没有内置 skill：

1. 调用 `list_follows`，读取关注对象的 `target_type`、`target_id`、名称、关注时间和近 7 天文章数，确认本周关注方向。
2. 调用 `follow_feed`，显式指定周报开始时间，例如 `{"since":"2026-10-05T00:00:00+08:00","limit":100,"collapse_similar":true}`。只整理某些对象时，增加 `target_ids`。省略 `since` 使用网页上次查看时间；MCP 调用不会更新该时间。since 比较进入动态时间，排序仍按收录时间倒序。每次收到 `next_cursor`，继续调用 `follow_feed`，传入 `{"cursor":"上一页的 next_cursor","limit":100}`，直到响应没有 `next_cursor`；再开始整理全部主项及折叠成员。
3. 根据返回的 `results`、`matched_follows` 和 `similar_reports` 归纳线索。需核实的全文文章用 `get_article` 分段读取；摘要加链接文章注明资料范围。文章 ID 用于引用和去重，勿把相似报道当作独立事件重复统计。
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

Mac mini 使用 Apple Silicon，安装 Git、Node.js/npm 和支持 Docker Compose 的 Docker 环境。应用、PostgreSQL（ParadeDB）与 Redis 使用 Compose；模型服务可以在 macOS 原生运行。应用镜像从当前仓库构建，未发布公共应用镜像。

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env
```

编辑 `.env`，至少确认以下配置：

| 配置 | 说明 |
| --- | --- |
| `VAULT_ROOT` | 容器内文件库路径，建议保持 `/data/vault` |
| `LOCAL_STORAGE_BASE_DIR` | 容器内上传资源目录，建议保持 `/data/files` |
| `DB_DRIVER`、`DB_HOST`、`DB_PORT` | 使用 `postgres`、Compose 服务名 `postgres` 与 `5432` |
| `DB_USER`、`DB_PASSWORD`、`DB_NAME` | PostgreSQL 账号、密码和库名，同时用于初始化数据库服务 |
| `REDIS_ADDR`、`REDIS_PASSWORD` | Compose 内通常使用 `redis:6379`，密码与 Redis 服务保持一致 |
| `JWT_SECRET`、`SYSTEM_AES_KEY` | 设置自己的登录签名密钥及 32 字节凭据加密密钥，迁移时保留加密密钥 |
| `OLLAMA_BASE_URL` | 容器访问 macOS 模型服务的地址，例如 `http://host.docker.internal:11434` |
| `OLLAMA_OPTIONAL` | 设为 `true` 时，Ollama 暂不可用只输出告警 |
| `SSRF_WHITELIST_EXTRA` | 部署额外放行的抓取或模型主机/IP/CIDR；与设置页白名单合并 |
| `APP_PORT`、`FRONTEND_PORT` | 宿主机 API 与网页端口，默认 `8080` 和 `80` |
| `APP_EXTERNAL_URL` | 需要外部调用方读取限时文件链接时填写内网访问地址并代理 `/r/` |

模型服务的完整地址、模型名、API Key、嵌入维度在“设置 > 模型”配置，知识库页选择具体档位。也可通过 [内置模型配置](BUILTIN_MODELS.md) 声明。迁移后需要检查数据库中保存的模型地址是否仍可访问。

Compose 默认使用 `vault-data` 与 `data-files` 命名卷。为便于 Obsidian 阅读与直接复制，建议将两者换为宿主机目录。先创建目录，再在 `.env` 添加实际的绝对路径：

```dotenv
RELIQUARY_VAULT_DIR=/Users/yourname/ReliquaryData/vault
RELIQUARY_FILES_DIR=/Users/yourname/ReliquaryData/files
VAULT_ROOT=/data/vault
LOCAL_STORAGE_BASE_DIR=/data/files
```

创建本地 `compose.mac.yml`，同一容器目标的卷配置会替换默认命名卷：

```yaml
services:
  app:
    volumes:
      - type: bind
        source: ${RELIQUARY_VAULT_DIR}
        target: /data/vault
      - type: bind
        source: ${RELIQUARY_FILES_DIR}
        target: /data/files
```

将 `.env` 中的目录替换为自己的路径后，执行：

```bash
mkdir -p /Users/yourname/ReliquaryData/vault /Users/yourname/ReliquaryData/files
export COMPOSE_FILE=docker-compose.yml:compose.mac.yml
export COMPOSE_PROJECT_NAME=reliquary
./scripts/build_frontend_dist.sh
docker compose build
docker compose run --rm --no-deps --user root --entrypoint sh app   -c 'chown -R appuser:appuser "$VAULT_ROOT" "$LOCAL_STORAGE_BASE_DIR"'
docker compose up -d
```

启动后访问 `.env` 配置的网页端口，注册唯一用户，配置模型并收录一篇文章。检查文件库是否产生 Markdown，知识检索是否命中。后续执行 Compose、备份或恢复时，使用相同的 `COMPOSE_FILE` 与 `COMPOSE_PROJECT_NAME`。

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

在“设置 > 模型”新增或编辑 embedding 模型，选择自定义 HTTP 服务，填写模型名、地址和维度，开启“支持图片”，请求格式选 Jina。虽然服务在本机，连接方式使用远程 HTTP 模型配置。然后在“设置 > 知识库”的模型页签选择该 embedding 模型。无需改变文章处理模型。全文保存文章的正文图片会生成图片向量，搜索命中时显示通过附件鉴权加载的缩略图；摘要加链接没有图片向量，用户文档和整理结果只生成文字分块向量。本地服务不下载输入图片 URL，应用传入已保存附件的 data URI。

图片通道沿用 RRF，默认权重 0.2，正文分块和文章向量通道各为 0.35，关键词为 0.3。权重无需合计为 1；图片用于补充召回，避免较粗的图像相似度压过匹配正文。在检索参数设置中可修改图片权重，填 0 使用默认 0.2；模型没有开启支持图片时通道关闭。

模型换成较大型号时，先在新服务确认文字和图片均返回同一模型的向量及正确维度，再创建新的 embedding 模型记录并选择它。使用新记录可明确区分模型身份，避免直接覆盖相同 ID 的权重。`--model` 可替换成其他具有 transformers 双塔特征接口的模型；其他架构需使用支持它的 Jina 请求格式服务，本地小服务不支持任意模型架构。

模型选择后旧模型向量立即退出向量召回，关键词检索持续可用，新向量逐项完成后加入召回。进入“设置 > 知识库”的模型页签，点击“重建全部向量”，后台显示总数、已完成和失败数。重建覆盖正文分块、文章标题与摘要、正文图片、用户文档与整理结果的分块、实体名称。可暂停后继续，服务重启后保留检查点；失败列表显示错误，可只重试失败项。已成功的检查点不会重复计算。模型配置变化后的旧任务不能继续，需用当前模型开始新重建。

命令行入口沿用 server 的数据库、模型、文件库和白名单配置。原生运行示例：

```sh
go run ./cmd/server reembed
go run ./cmd/server reembed --resume <任务ID>
go run ./cmd/server reembed --retry-failed <任务ID>
```

命令以 JSONL 输出进度，Ctrl+C 保存暂停状态。Compose 部署可在应用容器执行同一 `server reembed` 子命令。界面与命令行共用任务表，应继续已有任务，避免同时创建重建。

第一次启动包含约 1.5 GB 权重下载和加载时间。本机 Apple M4、16 GB 内存的 CPU 实测中，预热后的短文字约 26 ms、单图约 63 ms，28 个混合向量后台重建约 2 到 5 秒，峰值 RSS 约 1.5 GiB。相同规模输入的 1000 个向量可先按 1 到 3 分钟估计，长文窗口、其他进程负载、模型限流和网络会延长时间。完整收录、检索、重建记录见 `.scratch/kb-reshape/report-12.md`；不同模型和设备耗时不同。重建单位是向量数，长文章有多个分块，不能直接用文章数估算。可以用一小批实际数据的单项耗时乘以页面总数估计，再留出模型限流、磁盘读取和失败重试时间。默认模型文字上下文仅 64 tokens，本地服务对长文字分窗口编码并聚合，这只用于验证链路，未进行模型质量评测。

## 迁移到另一台 Mac mini

迁移传输当前版本的完整数据库、文件库、上传资源与部署配置。旧结构不支持通过此步骤升级，旧 WeKnora 或旧 Reliquary 数据仍需重新安装。两台机器使用相同代码版本、PostgreSQL 主版本及 ParadeDB 扩展版本，保持容器内文件库和资源路径一致；宿主机绑定目录可以不同。

上传资源的数据库记录可能包含绝对路径，如 `stored_resources.PhysicalPath` 与文章的 `FilePath`。修改宿主机目录时只改绑定挂载的 `source`，保持应用容器内 `LOCAL_STORAGE_BASE_DIR=/data/files`；单纯复制资源并改变容器内根目录不会自动修正这些数据库引用。文件库文章路径主要是相对路径，但完整迁移也建议保持容器内 `VAULT_ROOT=/data/vault`。

1. 在原机器暂停 RSS 同步与外部收录，等待运行队列完成并等待表头更新写入。暂停 Obsidian 和其他工具对文件库的编辑。记录文章数、星标、主题和代表性的检索结果。
2. 停止应用与网页，保留 PostgreSQL 运行。用脚本导出数据库并复制文件库和上传资源：

   ```bash
   docker compose stop app frontend
   ./scripts/backup.sh /Users/yourname/Backups/reliquary-2026-10-11
   ```

   脚本使用 `pg_dump --format=custom` 生成 `database.dump`，另复制 `vault/` 与 `resources/`，记录格式标识与 SHA-256 校验。备份目录必须不存在或为空，已有内容会被拒绝。完成后可 `docker compose stop postgres redis` 关闭剩余服务；若原机器继续使用，执行 `docker compose start app frontend`。
3. 将完整备份目录复制到新机器。另行复制 `.env`、Compose 配置、`config/config.yaml` 与自定义内置模型配置。`SYSTEM_AES_KEY` 必须保持一致，否则数据库里的模型 Key 和订阅凭据无法解密；登录签名配置也建议保持一致。脚本不把这些密钥写入备份清单。
4. 在新机器修改宿主机目录、数据库连接、网页/API 端口、模型地址及必要的 SSRF 白名单。准备新的空 PostgreSQL 数据目录与空文件库/资源目录，构建相同版本镜像。先启动数据库和 Redis，再创建处于停止状态的应用容器，避免自动迁移提前创建应用表：

   ```bash
   export COMPOSE_FILE=docker-compose.yml:compose.mac.yml
   export COMPOSE_PROJECT_NAME=reliquary
   docker compose build
   docker compose up -d --wait postgres redis
   docker compose create app
   ./scripts/restore.sh /Users/yourname/Backups/reliquary-2026-10-11
   ```

   恢复前脚本校验备份，拒绝运行中的应用、已有应用表或非空文件目录。`pg_restore` 用单个事务导入到目标库，去除原库角色所有权与权限要求；仅在已确认没有应用表的目标库清理同名扩展和结构。文件复制在数据库提交后进行，若失败应保持应用停止，检查错误并用新的空库和空目录重试。
5. 核对目录权限。默认命名卷恢复时，脚本会为镜像中的 `appuser` 修正权限；宿主机目录或自定义镜像由部署方检查。然后启动应用：

   ```bash
   docker compose run --rm --no-deps --user root --entrypoint sh app      -c 'chown -R appuser:appuser "$VAULT_ROOT" "$LOCAL_STORAGE_BASE_DIR"'
   docker compose up -d
   ```
6. 登录并核对文章数、选定文章的表头与正文、星标、主题名/说明及关联。重复原机器的关键词与向量检索，检查命中文章和用户文档。打开“设置 > 文件库状态”，等待扫描完成，核对 `mine/` 和 `reports/` 数量、最近扫描与错误；确认上传原始文件与图片可读取。

核对排序时，先让两侧索引统计一致。`pg_restore` 会重新创建全文索引；原库多次更新留下的死行可能影响 BM25 评分，进而改变混合检索排序。本次演练中仅 VACUUM 后分数仍有差异，两侧按相同版本重建全文索引后，完整检索响应一致。需要逐项比较评分与顺序时，可在原机器停止收录后、备份前，以及新机器导入后分别执行以下数据库维护，再比较相同查询。此维护清理和重建索引，不修改文章或检索配置，机制见 [ParadeDB 评分说明](https://www.paradedb.com/docs/reference/full-text/score#score-refresh)。

```bash
docker compose exec -T postgres sh -eu -c '
  psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -c "VACUUM (ANALYZE) public.knowledges;" \
    -c "VACUUM (ANALYZE) public.chunks;" \
    -c "REINDEX INDEX public.knowledges_summary_search_idx;" \
    -c "REINDEX INDEX public.chunks_search_idx;"
'
```

脚本用法为 `scripts/backup.sh <备份目录>` 和 `scripts/restore.sh <备份目录>`，均提供 `--help`。默认从停止的 Compose 应用容器读取或写入挂载目录，因此可以处理命名卷。显式设置以下变量时，脚本直接复制宿主机目录：

```bash
COMPOSE_FILE=docker-compose.yml:compose.mac.yml COMPOSE_PROJECT_NAME=reliquary   VAULT_ROOT=/Users/yourname/ReliquaryData/vault   LOCAL_STORAGE_BASE_DIR=/Users/yourname/ReliquaryData/files   ./scripts/backup.sh /Users/yourname/Backups/reliquary-2026-10-11
```

恢复时使用相同变量格式，路径改为新机器的空目标目录，命令改为 `restore.sh`。这里脚本的 `VAULT_ROOT` 和 `LOCAL_STORAGE_BASE_DIR` 覆盖值是宿主机路径，`.env` 内的应用配置仍可以使用 `/data/vault` 与 `/data/files`。数据库操作在 Compose 的 `postgres` 服务内执行，不需要宿主机安装 PostgreSQL 客户端；自定义服务名可设置 `APP_SERVICE`、`DB_SERVICE`。

### 只有文件库时重建

没有数据库备份时，保留文件库的完整目录和附件，复制到新机器。准备新数据库与 Redis，配置 `VAULT_ROOT`、模型地址和凭据加密密钥。保持应用停止，先启动数据库，然后运行专用重建命令：

```bash
docker compose up -d --wait postgres redis
docker compose run --rm --no-deps --user root --entrypoint sh app   -c 'chown -R appuser:appuser "$VAULT_ROOT" "$LOCAL_STORAGE_BASE_DIR"'
docker compose run --rm --no-deps app ./Reliquary vault-rebuild
docker compose up -d
```

命令使用应用相同的数据库配置和迁移，不启动 HTTP 服务或任务消费者，不修改文件库。知识数据库必须没有文章记录；配置和账号可以保留，完全空的数据库会创建个人空间与唯一知识库。输出 `articles`、`mine`、`reports` 和 `skipped`，逐项检查跳过的文件及原因。无法解析的文件会被跳过；如果中途重建失败，需要在空知识数据库修复后重试。

重建能恢复文章 ID、表头、正文、星标、收藏理由、内容类型、实体与主题名称及关联，并重新生成检索索引。表头保留 source_unverified 及特殊全文保留的 processing_mode，重建后仍可识别来源未核实与保留正文。表头没有实体类型、别名和说明、主题说明、账号、API Key、模型配置、审核状态、关注关系与关注动态查看时间、操作日志、转载记录与采集器专有注解，这些内容无法从文件库恢复。没有已知实体可归入时，重建实体类型为“其他”。收录待审内容尚未写入文件库，也无法恢复；处理待复核文章可以恢复正文，但复核状态无法恢复。

空数据库默认重建关键词索引，无需模型。若同时重建向量索引，可在 `.env` 配置 `VAULT_REBUILD_EMBEDDING_URL`、`VAULT_REBUILD_EMBEDDING_MODEL`、`VAULT_REBUILD_EMBEDDING_DIMENSION` 和必要的 `VAULT_REBUILD_EMBEDDING_API_KEY`，使用可访问的 OpenAI 兼容嵌入接口；已有模型配置时可设置 `VAULT_REBUILD_EMBEDDING_MODEL_ID`。确认模型主机已通过 SSRF 白名单。重建后对比文章数、表头中的用户判断、关键词/向量检索及文件库扫描状态。

## 外部 Agent 接入

Grok Bot、Claude Code 等调用方需能访问 Reliquary 的内网地址。在网页登录后打开“设置 > API 集成”，点击“创建 API Key”，填写名称及可选有效期，保存后复制完整令牌。完整令牌只在创建时显示，丢失后需创建新 Key；同页可修改名称、有效期或撤销 Key。该页同时提供收录接口与 MCP 地址的复制按钮。

HTTP 收录与 MCP 使用同一个 Key，通过 `Authorization: Bearer <key>` 发送。Key 具有个人空间的全部访问权限。此接入方式只用于内网，调用方负责采集正文、补全推文串与引用、记录收藏理由；Reliquary 负责去重、处理、存储与检索。

`POST /api/v1/ingest` 接收 JSON：

| 字段 | 含义 |
| --- | --- |
| `url`、`content` | 必填，原链接与 Markdown 或纯文本正文 |
| `title`、`author`、`publisher`、`published_at` | 可选，标题、作者、发布者与发布时间；时间使用 YYYY-MM-DD 或 RFC3339，未知时不填写 |
| `images` | 可选数组，每项在 `url` 与 `data` 中选择一个；`data` 为图片 base64，可另传 `caption` 图片说明与 `text` 图片中的文字 |
| `thread` | 可选推文串数组，每项有 `content`，可另传 `author` 与 `published_at` |
| `quotes` | 可选引用数组，每项有 `content`，可另传 `author` 与 `url` |
| `collect_reason` | 可选收藏理由，写入文章表头 |
| `agent_summary`、`agent_tags`、`source_name` | 可选调用方摘要、字符串标签数组、调用方名称，仅存数据库，不进入表头或处理调用的提示词 |

下面模拟 Grok Bot 发送正文、推文串、引用和图片。将地址与 Key 替换为实际值；通过反向代理子路径访问时，地址需包含该子路径。

```bash
export RELIQUARY_URL='http://192.168.1.20:8080'
export RELIQUARY_API_KEY='替换为创建时复制的完整令牌'
curl --fail-with-body --request POST "${RELIQUARY_URL}/api/v1/ingest" \
  --header "Authorization: Bearer ${RELIQUARY_API_KEY}" \
  --header 'Content-Type: application/json' \
  --data-binary @- <<'JSON'
{
  "url": "https://example.com/research/original-article",
  "content": "团队发布了新的检索模型，以下记录原文要点。",
  "title": "检索模型发布记录",
  "author": "Example Researcher",
  "publisher": "Example Lab",
  "published_at": "2026-10-11T08:00:00Z",
  "images": [
    {
      "data": "iVBORw0KGgoAAAANSUhEUgAAAIAAAACACAIAAABMXPacAAAA+UlEQVR4nO3RQQ0AIAzAwElCGPIRgIw9ekkFNLk592mxWT+IBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtAABoBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtAABoBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtAABoBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtAABoBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtAABoBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtAABoBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtAABoBwBAOwAA2gEA0A4AgHYAALQDAKAdAADtPtWtBd0rlTRDAAAAAElFTkSuQmCC",
      "caption": "评测图示例",
      "text": "召回率 92%"
    }
  ],
  "thread": [
    {
      "author": "Example Researcher",
      "published_at": "2026-10-11T08:05:00Z",
      "content": "补充推文：评测使用公开测试集。"
    }
  ],
  "quotes": [
    {
      "author": "Example Lab",
      "url": "https://example.com/evaluation",
      "content": "该结果来自固定测试集，尚未覆盖全部场景。"
    }
  ],
  "collect_reason": "对比下一轮知识库检索模型",
  "agent_summary": "Grok Bot 提供的采集摘要",
  "agent_tags": ["retrieval", "model"],
  "source_name": "grok-bot"
}
JSON
```

图片也可使用 `{"url":"https://example.com/chart.png","caption":"评测图","text":"召回率 92%"}`。下载沿用带 SSRF 防护的抓取，失败项跳过，响应的 `image_failures` 列出零起始的 `index`、`url` 与 `reason`。其余内容继续收录。

主内容经既有 Markdown 清理与首尾空白裁剪后放在正文开头，按固定次序追加非空的推文串、引用、成功图片。推文和引用从 1 开始编号，图片保留输入数组中的序号；可选作者、时间、来源、说明和图片文字仅在提供时输出。引用和图片文字逐行添加 Markdown 引用前缀。全文文章的最终正文示例：

```markdown
团队发布了新的检索模型，以下记录原文要点。

## 推文串

### 推文 1

作者：Example Researcher

时间：2026-10-11T08:05:00Z

补充推文：评测使用公开测试集。

## 引用

### 引用 1

作者：Example Lab

来源：https://example.com/evaluation

> 该结果来自固定测试集，尚未覆盖全部场景。

## 图片

### 图片 1

![评测图示例](<图片存储地址>)

说明：评测图示例

图片文字：

> 召回率 92%
```

外部 Agent 收录经过同一模型审核，调用方的判断、agent_summary 与 agent_tags 不会替代审核。审核拒绝时返回 HTTP 201、status: "pending_review" 与 reason，文章进入收录待审，正文不会写入文件库；通过后才进入异步处理。新文章通过时返回 HTTP 201，响应为 `{"success":true,"data":{"article_id":"...","status":"pending","message":"...","image_failures":[]}}`；可用同一个 Key 请求 `GET /api/v1/knowledge/<article_id>` 查看 `parse_status`。如果任务入队失败，返回状态为 `failed`。去重命中返回 HTTP 200、已有 `article_id`、`status: "exists"` 与 `message: "已存在"`，不再次处理；本次有收藏理由且原文章没有时，补写收藏理由并刷新表头。收录操作记录在操作日志中。正文和附件最终是否保留，仍由既有内容类型与处理方式决定。

MCP 服务使用 Streamable HTTP，路径为 `/mcp`。在可访问内网的 Claude Code 终端执行以下完整命令，使用上例设置的地址与 Key：

```bash
claude mcp add --transport http reliquary "${RELIQUARY_URL}/mcp" \
  --header "Authorization: Bearer ${RELIQUARY_API_KEY}"
claude mcp get reliquary
```

HTTP 连接与 Bearer 头的命令格式见 [Claude Code 官方 MCP 文档](https://code.claude.com/docs/en/mcp#option-1-add-a-remote-http-server)。MCP 无效 Key 会被拒绝。MCP 与 HTTP 调用相同服务层，写操作同样进入操作日志。工具清单：

| 工具 | 参数 | 返回 |
| --- | --- | --- |
| `search` | `query`；可选 `content_types`、`entity_ids`、`topic_ids`、`published_from`、`published_to`、`starred`、`sources`、`collapse_similar`、`limit` | 与知识检索 HTTP 接口相同的文章合并结果、片段、相似报道、source_unverified 及缺失发布时间排除数量 |
| `get_article` | `id`；可选 `offset`、`length`，按 Unicode 字符计数，默认 0 与 20000，长度最大 100000 | 表头、正文、处理方式、处理状态、source_unverified，以及 `offset`、`length`、`total_length`、可继续读取时的 `next_offset`；摘要加链接文章返回 `header.summary` 与 `original_url`，`body` 为空 |
| `list_topics` | 无 | `{topics: [...]}`，包含主题名称、说明与文章数 |
| `list_entities` | 可选 `entity` 名称或别名模糊查询、`entity_type` 类型、`limit`、`offset`；默认 50 条，最多 100 条 | `{entities: [...]}`，实体列表；类型为 `company`、`person`、`product`、`project`、`organization`、`other` |
| `get_entity` | `id` | 实体档案与提及该实体的文章列表 |
| `ingest` | 与上述收录接口相同 | 文章 ID、状态、待审原因及图片失败清单 |
| `list_pending` | 可选 `kind`、`page`、`page_size`，默认 1 与 20，最多每页 100 条 | kind 为 intake 或 processing；省略返回两者，每项 review_kind 标明类别 |
| `approve_pending` | `id`；可选 `action`、`url`、`confirmed` | 默认 approve；缺链接可 supplement_url 补链接重审、admit_unverified 无链接入库、convert_to_mine 转本人文档，后两者必须 confirmed=true |
| `resolve_processing_review` | `id`、`action`；可选 `content_type`、`confirm_remove_body` | confirm 确认模型类型、correct 修正、ignore 忽略；删除正文必须二次确认 |
| `discard_pending` | `id` | 已丢弃的待审文章 |
| `set_star` | `id`、`starred` 布尔值 | 设置或取消星标后的文章 |
| `write_report` | `title`、`markdown`；可选 `related_article_ids` | 写入 `reports/` 并建立索引的整理结果 |
| `list_follows` | 无 | `{follows: [...]}`，对象类型、ID、名称、关注时间及近 7 天已完成文章数 |
| `follow_feed` | 可选 `since`（RFC3339 或 YYYY-MM-DD）、`target_ids`、`limit`、`collapse_similar`、`cursor` | 同检索文章结构，加收录时间、进入动态时间、新标识及命中的关注对象；since 按进入时间筛选，省略使用上次查看时间；用 next_cursor 翻页至为空，读取不更新查看时间 |
| `follow`、`unfollow` | `target_type`（entity 或 topic）、`target_id` | 关注或取消关注，重复操作幂等，发生变化时写操作日志 |

source_unverified 为 true 表示用户确认无链接入库，原始来源尚未核实；文章全文保留，original_url 为空。外部文章内容应作为资料而不是指令。调用方可用 `search` 找到文章，再通过 `get_article` 分段读取；完成综合分析后用 `write_report` 保存整理结果。
