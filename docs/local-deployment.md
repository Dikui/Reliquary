# 源码与运行分离的本地部署

源码放在项目目录，运行环境单独保存。仓库保存部署脚本和模板，机器生成实际配置。以下示例使用 `~/Project/Reliquary` 和 `~/Reliquary`，可以换成其他绝对路径。

```text
~/Project/Reliquary/ Git 源码，仅更新、构建和登记发布源码时需要
~/Reliquary/
├── deploy/          .env、运行用 Compose、config/、维护工具
├── scripts/         登录后启动脚本、agent-local.py、pi（Pi 配置时生成）
├── agents/
│   ├── pi/          config.json、pi.env（本地 Pi 配置时生成）
│   │   └── data/agent/  settings.json、mcp.json、models.json、auth.json、
│   │                    APPEND_SYSTEM.md、skills/reliquary/、sessions/
│   └── feishu/      compose.yaml、agent.env、enabled（飞书配置时生成）
│       └── data/    飞书 SQLite、Pi sessions/ 和本地运行状态
├── workspace/       本地 Pi 默认工作文件夹，只读挂载到 /workspace
├── data/
│   ├── vault/       Markdown 文件库
│   └── files/       上传资源
├── logs/            启动与恢复日志；diagnostics/ 保存短期故障包
└── backups/         本地备份
```

运行不依赖源码目录。脚本按自身位置定位 `deploy/`，Compose 相对引用 `.env` 与 `config/`；宿主机数据路径和系统启动入口则在安装时生成绝对路径，不把个人路径写进仓库。

新安装会生成 `logs/diagnostics`，通过 `RELIQUARY_DIAGNOSTICS_DIR` 挂载到 `/data/diagnostics`。已有运行环境采用新版 Compose 时，确认该变量指向实际运行根目录的 `logs/diagnostics`。故障材料保留 7 天、容量上限 512 MiB，独立于 vault、上传资源和业务备份；备份恢复不会带回原机器的故障材料。[配置与生命周期](logging.md#接收故障日志与故障包)说明了过期和容量淘汰规则。

单独使用 `deploy-local.py` 时，在源码仓库的本机文件 `.deploy.env` 中记录部署位置，并在更新前核对。它不提交到 Git，也不保存运行凭据：

```sh
RELIQUARY_DEPLOY_DIR="$HOME/Reliquary/deploy"
```

Docker 管理镜像、容器日志和 PostgreSQL、Neo4j 数据卷。Ollama 模型可供多个项目共用，继续由 Ollama 管理。macOS 的 LaunchAgent 文件仍安装在 `~/Library/LaunchAgents/`，引用本目录的脚本。基础服务启动后无需保留打开的终端或 Codex 对话，本地 Pi 按需在终端启动。

## 客户全新安装

支持 macOS、Linux。安装 [Python 3.9+](https://www.python.org/downloads/)、[Git](https://git-scm.com/downloads) 和带 Compose v2 的 [Docker](https://docs.docker.com/get-started/get-docker/)，然后启动 Docker。Compose 需支持 `config --no-env-resolution`。系统安装、浏览器账号登录和飞书权限审批需要本人完成。后续运行依赖 Docker，引导脚本依赖 Python。

```sh
git clone https://github.com/Dikui/Reliquary.git
cd Reliquary
python3 scripts/install-local.py --install --root "$HOME/Reliquary"
```

安装依次执行以下操作。

1. 校验新的运行目录和未被占用的 Compose 项目名，生成随机数据库密码及加密密钥。
2. 构建 Go 后端和 Vue 网页。Docker 下载 Go、Node 和数据库基础镜像，宿主机无需安装 Go 或 Node。
3. 启动 PostgreSQL、Redis、Neo4j、后端和网页，等待健康检查。数据库迁移只作用于本次安装的独立数据库。
4. 打开本机网页，客户创建资料库。资料库本体到此安装完成。
5. 显示本地 Pi 的数据外流说明，询问是否配置，默认选择是；可跳过。非交互运行或跳过时打印 `pi-configure` 命令。Pi 配置失败不影响已完成的本体安装。

### 推荐：配置本地 Pi

启用前向客户说明并取得同意：每轮完整上下文（问题、工具读到的资料片段、工作文件夹中读到的内容、历史回复）发送给所选模型服务商；ChatGPT 订阅经 Reliquary 后端转发给 OpenAI。资料库正文、文件和数据库不会整体同步出去。决策与已知限制见 [ADR 0029](adr/0029-recommend-local-pi.md)。

安装时跳过后可单独运行；更换模型、工作文件夹或代理也使用同一命令：

```sh
python3 "$HOME/Reliquary/scripts/agent-local.py" pi-configure
```

首次配置从安装时登记的源码构建 Pi 镜像，宿主机无需安装 Node。若登记的源码已移动，从含 Pi 实现的源码仓库执行 `python3 scripts/agent-local.py pi-configure --root "$HOME/Reliquary"`。按提示选择客户 API 或已授权的 ChatGPT 模型、一个工作文件夹和容器可达的模型代理；已有飞书模型配置时可选择复用。

默认工作文件夹为 `~/Reliquary/workspace`，以只读方式挂载到容器的 `/workspace`；Pi 自身状态另挂载到 `/data`。配置写入 `agents/pi/config.json`、`pi.env` 和 `data/agent/` 下的工具、MCP、模型、凭据、规则及技能文件，并生成启动器 `scripts/pi`。内置工具只有 read，MCP 只连接 Reliquary，另有工作簿上传和原件暂存工具。表格必须按导入方案上传、审核确认，不自行拆成逐行写入。

向导会调用模型，要求实际成功调用 `list_libraries`，消耗少量额度。成功时显示“本地 Pi 已读到资料库”和回答；空库可通过。完整安装与只读会话验收见 [Agent 辅助安装指南](agent-installation.md#5-配置并验证本地-pi)。

### 可选：飞书入口

```sh
python3 "$HOME/Reliquary/scripts/agent-local.py" configure
```

需要飞书对话入口时再运行 `configure`（见 [ADR 0028](adr/0028-optional-feishu-entry.md)）：

1. 首次运行从安装时登记的源码构建飞书 Agent 镜像，`npm ci` 按锁文件下载 Pi Agent、Pi AI、Pi MCP、飞书 SDK 等依赖，并生成 `agents/feishu/` 运行配置。
2. 按提示选择资料库、模型并填写飞书应用配置。首次使用 ChatGPT 时，在 Pi 输出的授权页完成浏览器登录。已有 Pi 登录可继续使用。
3. 引导程序创建本机 Agent API Key，保存权限为 `0600` 的 `agent.env`，实际检查 Reliquary、模型工具调用和飞书应用身份。然后启动机器人，提示在目标群 @机器人发送验证消息。

启用后，群内问答经飞书服务器传输并保存在飞书；每轮对话上下文（问题、工具读到的资料片段、历史回复）发送给所选模型服务商。启用前须向使用者说明。

支持 `openai-completions`、`openai-responses` 和 `anthropic-messages` API。填写客户自己的端点、模型名、密钥、上下文和输出上限。ChatGPT 模式由 Pi 独立登录并直接调用模型，默认使用 `gpt-6.1-sol`。Pi 自行保存和续期 OAuth 凭据。Reliquary 提供知识库工具和技能，使用独立 API Key 访问。密钥和账号令牌属于运行数据，仓库只保存模板。

飞书需创建企业自建应用，启用机器人、长连接和 `im.message.receive_v1`，开通并发布所需权限，再加入指定群。`configure` 显示权限清单，并要求输入 App ID、App Secret、Tenant Key 和 Chat ID。使用本人现有租户即可。具体权限和群内操作见 [Agent 指南](../agents/feishu/README.md)。

默认占用本机 80、8080 和 6379 端口。网页和后端可用 `--frontend-port`、`--app-port` 修改。需要并行安装时，给 `--project-name` 指定新名字，并通过 `--env-file` 提供不同的 `REDIS_PORT`。命令因下载或构建失败中断时，修正原因后重跑同一安装命令。基础服务已经就绪时，重跑仍会引导配置本地 Pi，并提示可选的飞书 `configure`。

### 构建代理

构建镜像时，容器需要下载 Go 模块、系统包和 npm 依赖。宿主机代理工作在 TUN 或 fake-IP 模式时，容器内的 DNS 和 HTTPS 可能失败，例如 `lookup proxy.golang.org ... no such host`、`server gave HTTP response to HTTPS client`。此时在 `deploy/.env` 中填写一个容器可达的 HTTP 代理：

```sh
RELIQUARY_BUILD_PROXY=http://host.docker.internal:7897
# 可选，默认 localhost,127.0.0.1
RELIQUARY_BUILD_NO_PROXY=localhost,127.0.0.1
```

安装、按改动范围更新、`reliquary-admin upgrade` 和本地 Pi、飞书 Agent 构建都会读取这两项。首次安装时 `deploy/.env` 尚未生成，可在命令前临时设置同名环境变量。进程环境变量优先于 `deploy/.env`。代理通过 Docker 预置的 `HTTP(S)_PROXY`/`NO_PROXY` 构建参数传入，只在构建阶段生效，不写入镜像历史，也不影响构建缓存。app 容器通过 `env_file` 载入整个 `deploy/.env`，因此能看到 `RELIQUARY_BUILD_PROXY` 这个变量本身；后端不读取它，运行时网络只受 `HTTP(S)_PROXY` 影响，不会因此改走代理。留空时不传任何代理参数。

脚本不读取宿主机自身的 `HTTP_PROXY`：宿主机能用的地址，容器未必能连通。`host.docker.internal` 在 macOS 的 OrbStack、Docker Desktop 上指向宿主机。Linux 原生 Docker 需填写桥接网络可达的地址，并让代理软件监听该地址。

### 本地 Pi 运行与更新

```sh
"$HOME/Reliquary/scripts/pi"
"$HOME/Reliquary/scripts/pi" -c  # 继续上次会话
python3 "$HOME/Project/Reliquary/scripts/agent-local.py" pi-update --root "$HOME/Reliquary"
```

`scripts/pi` 每次在 Docker 中启动终端会话，退出后容器删除，配置与会话保留在 `agents/pi/data/agent/`。把待处理文件放到选定的工作文件夹，在对话中使用 `/workspace` 下的路径。

`pi-update` 必须从要发布的源码仓库执行。先退出正在运行的 Pi。命令重建镜像，刷新 `APPEND_SYSTEM.md`、技能包、启动器和 `models.json` 中的 Reliquary provider，再实际检查 `list_libraries` 调用。ChatGPT 使用 `pi.env` 中的 Reliquary API Key，客户 API 密钥继续存于 `auth.json`。字段或凭据不足时保留原模型文件，并提示运行 `pi-configure`。检查失败时恢复原镜像配置、启动器及本次改写的模型和凭据文件，已刷新的规则与技能继续生效。单独运行 `deploy-local.py` 时需另行更新 Pi。

`pi.env` 与 `auth.json` 含可用凭据，Pi 的 read 工具仍能读到容器内自己的凭据文件。`reliquary-admin` 暂不备份本地 Pi 的配置和会话，不能沿用下文飞书状态的恢复承诺；重新配置能恢复连接，不能恢复丢失的本地 Pi 会话。

### 飞书运行与更新

```sh
python3 "$HOME/Reliquary/scripts/agent-local.py" status
python3 "$HOME/Reliquary/scripts/agent-local.py" stop
python3 "$HOME/Reliquary/scripts/agent-local.py" start
python3 "$HOME/Project/Reliquary/scripts/agent-local.py" update --root "$HOME/Reliquary"
```

`status` 显示连接状态及收发记录数。`stop` 同时清除自动恢复标记。更新从源码构建新 Agent 镜像，保留会话目录，启动失败时恢复原 Compose 镜像。更新基础服务沿用下文的升级流程。客户正式使用干净的发布提交安装时会自动登记基础服务源码，供已有维护工具使用。未提交源码安装适用于本地测试，正式使用前应从发布提交重建并登记。

Reliquary 账号授权存于 PostgreSQL，加密密钥在 `deploy/.env`。Pi 独立授权存于 Agent 的 `data/auth.json`。Agent 的 SQLite 保存会话、任务、消息与确认关系，Pi 的 `data/sessions/` 保存实际模型对话。`./reliquary-admin backup` 在同一停机窗口内先停止 Agent、再暂停基础服务，把 `agents/feishu/data/`（不含 Pi 的 `auth.json` 和运行锁）连同数据库一起写入备份包的 `agent/`，结束后按 `enabled` 恢复运行。`agent.env` 中的模型密钥、飞书 App Secret 和 Agent API Key 不进入备份包，状态中若出现这些值，备份会被拒绝。`restore` 到标准运行目录时写回 `agents/feishu/data/`，不启动 Agent，检查清单提示运行 `configure` 重填凭据后再 `start`。恢复演练会检查 Agent SQLite 的完整性。未启用飞书入口的安装，备份与恢复行为不变。迁移和镜像回退见 [Agent 指南](../agents/feishu/README.md#迁移旧状态)。

macOS 登录后自动恢复可按下节注册，已启用的 Agent 会随基础服务恢复。Linux 重启策略由 Docker 的 `restart: unless-stopped` 执行，Docker 服务需在开机时启动。仅休眠或关闭终端无需重新授权。

## 仅准备运行目录

需要 Python 3.9+、Git、Node.js/npm、Docker Compose v2（支持 `config --no-env-resolution`）。macOS 自启动支持 OrbStack 和 Docker Desktop；如需本机 Ollama，同时安装 Ollama 桌面应用并准备模型。其他系统可使用相同运行目录，自启动需自行配置。

1. 将仓库克隆到 `~/Project/Reliquary`，启动 Docker，然后生成运行配置：

   ```sh
   cd ~/Project/Reliquary
   python3 scripts/install-local.py --root "$HOME/Reliquary"
   ```

   脚本先在临时目录验证配置，通过后才生成 `deploy/`；失败后可修正原因并重新运行。脚本生成随机数据库密码和 32 字节加密密钥。检查 `~/Reliquary/deploy/.env` 中的端口、镜像版本和模型地址。使用 ChatGPT 时按[个人使用指南](personal-use.md#使用-chatgpt-订阅)配置白名单并完成授权。脚本不自动修改模型策略或系统安全设置。

2. 确认源码已提交且工作区干净后构建镜像，再从运行目录启动：

   ```sh
   cd ~/Project/Reliquary
   ./scripts/build-frontend-dist.sh
   RELIQUARY_COMMIT="$(git rev-parse HEAD)" \
     docker compose --env-file "$HOME/Reliquary/deploy/.env" build app frontend
   cd "$HOME/Reliquary/deploy"
   ./reliquary-admin check-deployment
   docker compose up -d --wait postgres redis neo4j
   ./reliquary-admin check-deployment
   docker compose up -d --no-build --wait
   ./reliquary-admin record-runtime --source "$HOME/Project/Reliquary"
   ```

   运行 Compose 从源码模板生成，保留环境变量引用，去掉 `build`，应用镜像禁止自动拉取。维护工具也复制到 `deploy/`。`record-runtime` 按应用镜像的提交标记保存源码、Git bundle 和文件删除清单，绑定镜像 ID 并记录文件校验值，写入 `deploy/.maintenance-runtime/`。它不停止服务；登记后备份也无需保留源码仓库。

3. macOS 注册登录后启动。使用本机 Ollama 时加 `--ollama`：

   ```sh
   python3 ~/Project/Reliquary/scripts/install-local.py \
     --root "$HOME/Reliquary" --install-autostart --ollama
   ```

   任务每分钟检查一次；Docker 尚未启动时，`orbstack` context 使用 `orb start` 启动 OrbStack 引擎，其他 context 保留打开 Docker Desktop 的行为。启动引擎后等待下次检查恢复服务。缺少服务时使用已有镜像恢复五个容器。启用 `--ollama` 时也检查并打开 Ollama。脚本不更新源码、下载模型或构建镜像。启动日志位于 `logs/`。

   注册时将当前 Docker context 固定为 LaunchAgent 的 `DOCKER_CONTEXT`，后续 `docker context use` 不会改变该任务。直接运行脚本时优先使用 `DOCKER_CONTEXT`，未设置时读取 `docker context show`。迁移至 OrbStack 后，须将任务中的 `DOCKER_CONTEXT` 更新为 `orbstack` 并重新加载；同时将源码的 `scripts/startup-local.py` 同步到运行目录的 `scripts/startup.py`，仅更新源码不会更新已安装的副本。

4. 打开本机网页（默认 `http://127.0.0.1`），配置模型并测试检索。正常重启后，在未手动打开 Docker/Ollama 的情况下检查网页。无人值守来电恢复还需要在系统设置完成来电启动、禁止系统休眠，以及符合自身安全需求的自动登录配置；脚本不会代办这些系统设置。

## 已有安装整理

先停止旧的自启动任务与应用写入，保留原 `.env` 和启动配置快照。使用 `--env-file /原路径/.env` 生成新部署，凭据会原样保留，只替换宿主机数据路径和 Compose 项目名。复用数据库卷时必须保持原项目名（默认 `reliquary`），不要执行 `down -v`。

生成后，将原文件库和上传目录分别移入 `data/vault`、`data/files`，核对文件内容，再从 `deploy/` 启动。已有备份设置也应改为新的 `backups/`。新启动任务验证成功后，归档旧任务，避免同时运行两个恢复任务。脚本拒绝覆盖已有 `deploy/`，不会自动迁移已有资料或重置数据库。

## 已有安装更新

适用于通过 `install-local.py --install` 完成的客户安装。退出正在运行的本地 Pi，在安装时登记的源码工作副本中执行以下命令。

```sh
git pull
python3 scripts/install-local.py --update --root "$HOME/Reliquary"
```

更新命令非交互，使用当前分支已提交的源码，输出分支、提交和版本。无迁移时按改动范围更新前后端。有迁移时先从源码构建两者，再暂停写入、导出旧采集记录、执行并验证迁移，检查新容器并登记镜像对应的提交和源码，然后恢复写入。两条路径都在 Docker 内构建网页。基础服务完成后更新已配置的 Pi 和飞书。Pi 会试调用模型，启用中的飞书会等容器就绪，已停用的飞书会在下次启动时应用新镜像。

默认不备份，必要迁移照常执行。明确需要本次升级前恢复点时，在命令末尾加 `--backup`，只在走数据库升级时创建并校验完整备份。月度计划独立执行。

更新汇总会列出运行配置变化。新增环境变量有可用的非机密默认值时补入 `deploy/.env`。已知可在本机生成的机密变量使用随机值，其他凭据需按升级说明填写。客户已有值保留，模板删除的键和变更的默认值只在汇总中列出。

运行 Compose 与已部署版本的生成结果一致时，脚本会从新模板生成配置并保留现有镜像引用。基础服务、网络、端口、应用挂载或依赖发生变化时需人工核对。源码 `config/` 中发生变化的文件按旧版本内容核对后更新，运行提示词目录由客户管理。遇到自定义 Compose 覆盖、同一文件的客户改动或无法自动生成的机密变量，脚本会在切换服务前停止。需人工合并时，先核对升级说明及生效配置，再按下文的手动发布流程应用与验证。自动变更前的旧文件保存在运行目录 `.maintenance-schedule/runtime-config-*`，基础服务更新失败时恢复旧文件。

有版本说明时，命令打印当前运行版本之后、目标版本及之前的对应章节，停止时也会显示。根据说明完成所需人工操作和验证。源码有未提交改动、登记路径不符或后端基准不可确认时先修正基准。[升级说明](upgrade-notes.md)

普通部署或入口检查失败时，按输出确认回退结果，修正原因后可重跑更新。迁移、验证或切换中断并提示有待完成的升级时，保持现场，先检查容器日志和数据库迁移状态。确认可继续后，从源码执行 `RELIQUARY_ADMIN_ROOT="$HOME/Reliquary/deploy" ./reliquary-admin upgrade`，成功后再执行一键更新以补齐入口。需要退回时按[升级与失败退回](personal-use.md#升级与失败退回)从已有备份恢复到空环境。未做本次备份时，最近恢复点之后的数据可能丢失。

## 日常维护与升级

所有运行操作都在 `deploy/` 执行，例如 `docker compose ps`、`docker compose logs --tail 100 app` 和 `./reliquary-admin backup`。备份命令的具体恢复范围见[个人使用指南](personal-use.md)。同一硬盘的备份仍需另行复制到外置盘或其他设备。

部署与备份独立执行：

- 代码更新、全量构建与数据库升级默认不创建备份。客户安装可使用上文的一键更新，单独部署基础服务按下文“按改动范围更新”执行。
- 清库、删除数据卷、覆盖存储或大规模不可逆改写前，按是否需要恢复操作前状态决定是否单独备份。需要时先获得本次备份授权并校验备份；不能把部署或迁移请求当作备份授权。

发版时，表结构变化必须提供迁移及真实 PostgreSQL 测试。运行配置变化若无法由 `install-local.py --update` 自动处理，必须在对应版本记录人工操作和验证方法。[升级说明](upgrade-notes.md)

月度备份和恢复演练按独立计划执行，不与每次部署绑定。当前本机约定为北京时间每月 1 日 03:00 备份、13:00 恢复演练；计划配置和宿主机调度任务必须分别安装，保存配置不代表任务已注册。没有本次操作前的备份时，恢复只能使用已有的恢复点，可能丢失该恢复点之后的数据。

需要长期停服时，先卸载恢复任务，再停止容器，否则每分钟的检查会重新启动服务：

```sh
launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.reliquary.autostart.plist"
cd ~/Reliquary/deploy
docker compose stop
```

恢复时用 `launchctl bootstrap` 加载同一 plist。手动发布时，先按上述约定判断是否需要完整备份，再更新源码并构建新版本镜像。核对新版 Compose 模板与运行配置的差异，手动同步所需配置。有迁移时，在启动新版本前按[显式升级流程](personal-use.md#升级与失败退回)执行。仅手动切换镜像的发布可调整镜像引用并启动，随后从运行目录执行 `./reliquary-admin record-runtime --source /源码仓库的绝对路径` 登记源码材料。

旧部署缺少源码登记且需要使用备份时，可将新版 `scripts/reliquary_admin.py` 复制到 `deploy/scripts/`，再执行一次 `record-runtime`。备份会在暂停任务和停服前检查源码材料的完整性、镜像是否存在，以及登记的镜像 ID 是否与当前应用一致；检查失败时提示修复，不进入停服步骤。

### 按改动范围更新

完成上述安装并提交源码后，在源码仓库运行：

```sh
python3 scripts/deploy-local.py --dry-run  # 只读查看本次更新范围
python3 scripts/deploy-local.py            # 按改动范围更新
python3 scripts/deploy-local.py --all      # 手动全量构建、更新前后端
```

入口从源码根目录的 `.deploy.env` 读取 `RELIQUARY_DEPLOY_DIR`；在新 worktree 中需创建该本机文件，也可传 `--deploy-dir /实际运行目录/deploy`。源码有未提交改动时拒绝部署，确保镜像与登记的提交一致。

脚本分别比较前端、后端已部署的提交与当前提交：普通前端文件只更新前端，普通 Go 文件只更新后端；接口目录（`frontend/src/api/`、`internal/handler/`、`internal/router/`）和未知路径保守选择前后端。仅提示词模板（`config/prompt_templates/`）、文档、测试、CI 或代理规则变化时跳过构建。前后端基准各自保存并绑定镜像 ID，前端单独更新不会推进后端基准；前端缺少有效基准时选择全量，后端基准无法确认时拒绝更新，避免漏过数据库迁移检查。

普通更新复用前端依赖；`package.json`、锁文件、`.npmrc` 或 Node 版本、平台变化时重新执行 `npm ci`。首次启用复用时也会执行一次干净安装，指纹保存在 `node_modules/.reliquary-deps`。单独调用 `./scripts/build-frontend-dist.sh` 以及手动 `--all` 仍执行干净安装。Go 镜像构建保留模块下载与编译缓存，全量构建也可复用有效的 Docker 缓存。

所有选定镜像构建成功后才切换运行镜像。运行目录只维护一个 `.maintenance-schedule/local-deploy.images.json` 入口，并保留原有 Compose 覆盖配置；重复更新不会追加同名入口。只重建选定容器，等待健康检查、验证前端代理链路并核对实际镜像，不重启 PostgreSQL、Redis 和 Neo4j。后端单独更新时重载 Nginx，刷新后端地址。后端更新后登记源码；前端单独更新先核对已有源码材料，保留材料并跳过重复归档。部署与已安装的启动恢复、备份共用锁。更新失败时恢复原配置、维护工具和原容器；若恢复也失败，命令返回错误，须修复后再使用。

`--all` 表示全量更新两个应用服务，不负责首次安装、数据库迁移或覆盖机器配置。快速部署的运行覆盖配置将 `AUTO_MIGRATE` 设为 `false`，避免启动时执行迁移。发现 `migrations/` 变化时，任何模式均停止并提示执行显式升级流程。升级默认不备份，只有明确选择 `upgrade --backup` 才创建升级前备份。发现 `config/`（不含 `prompt_templates/`）或 `docker-compose.yml` 变化时，停止并提示按本节原有手动发布流程核对、同步运行配置。首次安装仍按前文执行。脚本不清理历史镜像和备份。

运行配置的变化仅涉及 `.env.example` 时，脚本在构建前为新增且尚未配置的键补入非空的非机密默认值，或生成可由本机创建的随机密钥。脚本列出补入的键名，保留已有值，并报告模板中的键删除和默认值变更。缺少外部凭据时停止，提示先在 `deploy/.env` 中填写对应的键。`--dry-run` 只预览配置变更。写入时在运行目录的 `.maintenance-schedule/runtime-config-*` 中保留原 `.env`，更新失败时恢复原文件。

### 提示词配置

仓库的 `config/prompt_templates/*.yaml` 保存提示词及 Git 修改历史。新安装会复制到 `deploy/config/prompt_templates/`，容器只读挂载该目录，后端启动时加载。普通代码部署和 `--all` 都不覆盖运行副本；只有模板变化时不会构建或重启服务。输出结构、工具权限等约束仍由代码维护。

已有安装首次采用此功能时，将模板目录复制到 `deploy/config/`，并在运行 Compose 的 `app.volumes` 中加入以下挂载。保留其他运行配置，按手动发布流程更新镜像并重建 app 容器，使新挂载生效；后续只改提示词时重启即可。

```yaml
- type: bind
  source: ./config/prompt_templates
  target: /app/config/prompt_templates
  read_only: true
  bind:
    create_host_path: false
```

日常修改仓库中的对应模板，用几条固定资料确认效果后提交 Git，再将该文件复制到运行目录并执行 `docker compose restart app`。例如在源码根目录执行：

```sh
. ./.deploy.env
cp config/prompt_templates/processing.yaml "$RELIQUARY_DEPLOY_DIR/config/prompt_templates/"
(cd "$RELIQUARY_DEPLOY_DIR" && docker compose restart app)
```

后端会检查文件格式和必需模板；缺失或无效时启动失败，不会偷偷改用镜像内的模板。检查 app 日志及健康状态，并抽查一次实际处理。效果不好时，从 Git 取出上次使用的文件，写回运行目录并重启；已经写入的处理结果不会自动恢复。程序升级若新增必需模板或修改配置格式，须明确迁移，不能仅保留不兼容的旧文件。

若移动运行目录，须同步更新 `.deploy.env`、`deploy/.env` 中的数据路径、系统已注册的启动路径，以及已保存的备份目录设置。不要只改文件夹名称。

只提交源码、脚本、示例配置和文档。`deploy/.env`、业务数据、授权、日志及备份不进入 Git，也不推送远程仓库。
