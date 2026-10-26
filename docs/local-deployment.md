# 源码与运行分离的本地部署

源码放在项目目录，运行环境单独保存。仓库保存部署脚本和模板，机器生成实际配置。以下示例使用 `~/Project/Reliquary` 和 `~/Reliquary`，可以换成其他绝对路径。

```text
~/Project/Reliquary/ Git 源码，仅更新、构建和登记发布源码时需要
~/Reliquary/
├── deploy/          .env、运行用 Compose、config/、维护工具
├── scripts/         登录后启动脚本
├── data/
│   ├── vault/       Markdown 文件库
│   └── files/       上传资源
├── logs/            启动与恢复日志
└── backups/         本地备份
```

运行不依赖源码目录。脚本按自身位置定位 `deploy/`，Compose 相对引用 `.env` 与 `config/`；宿主机数据路径和系统启动入口则在安装时生成绝对路径，不把个人路径写进仓库。

旧版本生成的 `logs/diagnostics` 目录和 `.env` 中的 `RELIQUARY_DIAGNOSTICS_DIR` 已不再使用，可手动删除。

在源码仓库的本机文件 `.deploy.env` 中记录部署位置，后续更新部署前先读取该文件。它不提交到 Git，也不保存运行凭据：

```sh
RELIQUARY_DEPLOY_DIR="$HOME/Reliquary/deploy"
```

Docker 管理镜像、容器日志和 PostgreSQL 数据卷。Ollama 模型可供多个项目共用，继续由 Ollama 管理。macOS 的 LaunchAgent 文件仍安装在 `~/Library/LaunchAgents/`，引用本目录的脚本。启动无需保留打开的终端或 Codex 对话。

## 新机器安装

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
   docker compose up -d --wait postgres redis
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

## 日常维护与升级

所有运行操作都在 `deploy/` 执行，例如 `docker compose ps`、`docker compose logs --tail 100 app` 和 `./reliquary-admin backup`。备份命令的具体恢复范围见[个人使用指南](personal-use.md)。同一硬盘的备份仍需另行复制到外置盘或其他设备。

部署与备份独立执行：

- 代码更新、全量构建与数据库升级默认不创建备份。代码更新按下文“按改动范围更新”执行；数据库变更仍使用显式升级流程，不跳过必要迁移。
- 清库、删除数据卷、覆盖存储或大规模不可逆改写前，按是否需要恢复操作前状态决定是否单独备份。需要时先获得本次备份授权并校验备份；不能把部署或迁移请求当作备份授权。

月度备份和恢复演练按独立计划执行，不与每次部署绑定。当前本机约定为北京时间每月 1 日 03:00 备份、13:00 恢复演练；计划配置和宿主机调度任务必须分别安装，保存配置不代表任务已注册。没有本次操作前的备份时，恢复只能使用已有的恢复点，可能丢失该恢复点之后的数据。

需要长期停服时，先卸载恢复任务，再停止容器，否则每分钟的检查会重新启动服务：

```sh
launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.reliquary.autostart.plist"
cd ~/Reliquary/deploy
docker compose stop
```

恢复时用 `launchctl bootstrap` 加载同一 plist。手动发布时，先按上述约定判断是否需要完整备份，再更新源码并构建新版本镜像；核对新版 Compose 模板与运行配置的差异，再调整 `deploy/.env` 镜像标签并启动。每次手动发布后，从运行目录执行 `./reliquary-admin record-runtime --source /源码仓库的绝对路径`，登记新镜像对应的源码材料。安装脚本不自动覆盖现有配置，也不提供自动升级。

已有安装需先将新版 `scripts/reliquary_admin.py` 复制到 `deploy/scripts/`，再执行一次 `record-runtime`。备份会在暂停任务和停服前检查源码材料的完整性、镜像是否存在，以及登记的镜像 ID 是否与当前应用一致；检查失败时提示修复，不进入停服步骤。

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

所有选定镜像构建成功后才切换运行配置。运行目录只维护一个 `.maintenance-schedule/local-deploy.images.json` 入口，并保留原有 Compose 覆盖配置；重复更新不会追加同名入口。只重建选定容器，等待健康检查、验证前端代理链路并核对实际镜像，不重启 PostgreSQL 和 Redis。后端单独更新时重载 Nginx，刷新后端地址。后端更新后登记源码；前端单独更新先核对已有源码材料，保留材料并跳过重复归档。部署与已安装的启动恢复、备份共用锁。更新失败时恢复原配置、维护工具和原容器；若恢复也失败，命令返回错误，须修复后再使用。

`--all` 表示全量更新两个应用服务，不负责首次安装、数据库迁移或覆盖机器配置。快速部署的运行覆盖配置将 `AUTO_MIGRATE` 设为 `false`，避免启动时执行迁移。发现 `migrations/` 变化时，任何模式均停止并提示执行显式升级流程；升级默认不备份，只有明确选择 `upgrade --backup` 才创建升级前备份。发现 `config/`（不含 `prompt_templates/`）、`docker-compose.yml` 或 `.env.example` 变化时，停止并提示按本节原有手动发布流程核对、同步运行配置。首次安装仍按前文执行。脚本不清理历史镜像和备份。

### 提示词配置

日常在 **设置 → 提示词** 中查看和编辑资料理解、长文合并、四类库的补充规则及评测出题提示词。保存内容写入现有 `system_settings` 表，立即用于后续模型调用，重启后保留；不需要同步文件或重启。已有资料不会自动重新处理。保存失败时，原生效内容不变。页面修改随数据库备份与恢复，不写回 YAML 或 Git。

仓库的 `config/prompt_templates/*.yaml` 保存文件默认值及其 Git 修改历史。未在页面保存过的模板使用这些默认值。新安装会复制到 `deploy/config/prompt_templates/`，容器只读挂载该目录，后端启动时加载。普通代码部署和 `--all` 都不覆盖运行副本；只有模板变化时不会构建或重启服务。输出结构、工具权限等约束仍由代码维护，页面只编辑现有模板的正文。

已有安装首次采用此功能时，将模板目录复制到 `deploy/config/`，并在运行 Compose 的 `app.volumes` 中加入以下挂载。保留其他运行配置，按手动发布流程更新镜像并重建 app 容器，使新挂载生效；后续只改提示词时重启即可。

```yaml
- type: bind
  source: ./config/prompt_templates
  target: /app/config/prompt_templates
  read_only: true
  bind:
    create_host_path: false
```

需要修改文件默认值时，在仓库中修改对应模板，用几条固定资料确认效果后提交 Git，再将该文件复制到运行目录并执行 `docker compose restart app`。页面保存的内容仍优先于文件默认值。例如在源码根目录执行：

```sh
. ./.deploy.env
cp config/prompt_templates/processing.yaml "$RELIQUARY_DEPLOY_DIR/config/prompt_templates/"
(cd "$RELIQUARY_DEPLOY_DIR" && docker compose restart app)
```

后端会检查文件格式和必需模板；缺失或无效时启动失败，不会偷偷改用镜像内的模板。检查 app 日志及健康状态，并抽查一次实际处理。页面修改效果不好时，在页面重新编辑并保存；仅回退文件默认值时，从 Git 取出上次使用的文件，写回运行目录并重启。已经写入的处理结果不会自动恢复。程序升级若新增必需模板或修改配置格式，须明确迁移，不能仅保留不兼容的旧文件。

若移动运行目录，须同步更新 `.deploy.env`、`deploy/.env` 中的数据路径、系统已注册的启动路径，以及已保存的备份目录设置。不要只改文件夹名称。

只提交源码、脚本、示例配置和文档。`deploy/.env`、业务数据、授权、日志及备份不进入 Git，也不推送远程仓库。
