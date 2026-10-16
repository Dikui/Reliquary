# 源码与运行分离的本地部署

长期使用推荐一个总目录。仓库保存部署脚本和模板，机器生成实际配置。以下示例使用 `~/Reliquary`，可以换成其他绝对路径。

```text
~/Reliquary/
├── source/          Git 源码，仅更新和构建时需要
├── deploy/          .env、运行用 Compose、config/、维护工具
├── scripts/         登录后启动脚本
├── data/
│   ├── vault/       Markdown 文件库
│   └── files/       上传资源
├── logs/            启动与恢复日志
└── backups/         本地备份
```

已有工作区可以保留原物理位置，用总目录下的 `source` 符号链接指向它；运行不依赖这个链接。脚本按自身位置定位 `deploy/`，Compose 相对引用 `.env` 与 `config/`；宿主机数据路径和系统启动入口则在安装时生成绝对路径，不把个人路径写进仓库。

Docker 管理镜像、容器日志和 PostgreSQL、Neo4j 数据卷。Ollama 模型可供多个项目共用，继续由 Ollama 管理。macOS 的 LaunchAgent 文件仍安装在 `~/Library/LaunchAgents/`，引用本目录的脚本。启动无需保留打开的终端或 Codex 对话。

## 新机器安装

需要 Python 3.9+、Git、Node.js/npm、Docker Compose v2（支持 `config --no-env-resolution`）。macOS 自启动使用 Docker Desktop；如需本机 Ollama，同时安装 Ollama 桌面应用并准备模型。其他系统可使用相同运行目录，自启动需自行配置。

1. 将仓库克隆到 `~/Reliquary/source`，启动 Docker，然后生成运行配置：

   ```sh
   cd ~/Reliquary/source
   python3 scripts/install-local.py --root "$HOME/Reliquary"
   ```

   脚本先在临时目录验证配置，通过后才生成 `deploy/`；失败后可修正原因并重新运行。脚本生成随机数据库密码和 32 字节加密密钥。检查 `~/Reliquary/deploy/.env` 中的端口、镜像版本和模型地址。使用 ChatGPT 时按[个人使用指南](personal-use.md#使用-chatgpt-订阅)配置白名单并完成授权。脚本不自动修改模型策略或系统安全设置。

2. 从源码构建镜像，再从运行目录启动：

   ```sh
   cd ~/Reliquary/source
   ./scripts/build-frontend-dist.sh
   docker compose --env-file "$HOME/Reliquary/deploy/.env" build app frontend
   cd "$HOME/Reliquary/deploy"
   ./reliquary-admin check-deployment
   docker compose up -d --wait postgres redis neo4j
   ./reliquary-admin check-deployment
   docker compose up -d --no-build --wait
   ```

   运行 Compose 从源码模板生成，保留环境变量引用，去掉 `build`，应用镜像禁止自动拉取。日常启动、模型授权和数据读写均不依赖 `source/`。维护工具也复制到 `deploy/`；需要源码的构建、开发和源码导出仍在 `source/` 进行。

3. macOS 注册登录后启动。使用本机 Ollama 时加 `--ollama`：

   ```sh
   python3 ~/Reliquary/source/scripts/install-local.py \
     --root "$HOME/Reliquary" --install-autostart --ollama
   ```

   任务每分钟检查一次；Docker 尚未启动时打开 Docker Desktop，缺少服务时使用已有镜像恢复五个容器。启用 `--ollama` 时也检查并打开 Ollama。脚本不更新源码、下载模型或构建镜像。启动日志位于 `logs/`。

4. 打开本机网页（默认 `http://127.0.0.1`），配置模型并测试检索。正常重启后，在未手动打开 Docker/Ollama 的情况下检查网页。无人值守来电恢复还需要在系统设置完成来电启动、禁止系统休眠，以及符合自身安全需求的自动登录配置；脚本不会代办这些系统设置。

## 已有安装整理

先停止旧的自启动任务与应用写入，保留原 `.env` 和启动配置快照。使用 `--env-file /原路径/.env` 生成新部署，凭据会原样保留，只替换宿主机数据路径和 Compose 项目名。复用数据库卷时必须保持原项目名（默认 `reliquary`），不要执行 `down -v`。

生成后，将原文件库和上传目录分别移入 `data/vault`、`data/files`，核对文件内容，再从 `deploy/` 启动。已有备份设置也应改为新的 `backups/`。新启动任务验证成功后，归档旧任务，避免同时运行两个恢复任务。脚本拒绝覆盖已有 `deploy/`，不会自动迁移已有资料或重置数据库。

## 日常维护与升级

所有运行操作都在 `deploy/` 执行，例如 `docker compose ps`、`docker compose logs --tail 100 app` 和 `./reliquary-admin backup`。备份命令的具体恢复范围见[个人使用指南](personal-use.md)。同一硬盘的备份仍需另行复制到外置盘或其他设备。

需要长期停服时，先卸载恢复任务，再停止容器，否则每分钟的检查会重新启动服务：

```sh
launchctl bootout "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.reliquary.autostart.plist"
cd ~/Reliquary/deploy
docker compose stop
```

恢复时用 `launchctl bootstrap` 加载同一 plist。升级前备份，更新 `source/` 并构建新版本镜像；核对新版 Compose 模板与运行配置的差异，再调整 `deploy/.env` 镜像标签并启动。安装脚本不自动覆盖现有配置，也不提供自动升级。若移动总目录，须同步更新 `.env` 中的数据路径和系统已注册的启动路径。

只提交源码、脚本、示例配置和文档。`deploy/.env`、业务数据、授权、日志及备份不进入 Git，也不推送远程仓库。
