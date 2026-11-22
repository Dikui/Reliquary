# Agent 辅助安装指南

客户拿到仓库后，将下列指令交给能访问本机终端和浏览器的 Agent。本指南用于在客户自己的电脑安装 Reliquary 资料库本体，并配置、验证推荐的本地 Pi 入口；客户需要群聊对话入口时再接通群聊。本地 Pi 和群聊均可跳过。

> 请先阅读 AGENTS.md 和 docs/agent-installation.md，按指南在本机安装资料库本体，复用已有基础软件。说明本地 Pi 的数据外流范围并取得我同意后，自动下载 Pi 及项目依赖，引导我选择自己的 API 或 ChatGPT 订阅、只读工作文件夹，配置本地 Pi 并验证工具调用、首次提问与继续会话；允许跳过。需要群聊入口时再配置群聊并验证实际消息。遇到需要我登录、输入密钥或批准权限的步骤，请打开准确页面并说明操作，完成后继续。保留已有部署和业务数据，完成后给我安装位置、验证结果和日常启动命令。

## 执行约定

- 先核对客户本次授权，已授权的安装与验证连续完成。系统提权、账号登录及平台审批按当前环境的权限机制处理。
- 默认安装到客户本机。网页与后端沿用回环监听。远程服务器操作浏览器时，先确认访问路径。[本机访问说明](adr/0015-no-login-local-only.md)、[ChatGPT 回调说明](personal-use.md#使用-chatgpt-订阅)
- 使用本指南中的现有脚本。源码、实际凭据、业务数据和会话分别保存，路径见第 3 步。
- 备份遵循根目录 AGENTS.md 的部署约定。全新安装只初始化自己的数据库和目录。检测到已有环境时先识别其归属，恢复同一次安装，或选择独立目录、项目名和端口。
- API Key、App Secret、OAuth token 只进入客户本机的私有配置。读取密钥时关闭终端回显，日志、截图和验收报告中的凭据统一脱敏。
- 安装阶段以只读查询验收。客户要求验证资料检索时，只向其指定的安装验收库写入明确标注的示例。现有业务资料保留原样。

## 1. 核对源码

先定位下载目录，读取根目录 AGENTS.md，核对以下文件存在。

```text
scripts/install-local.py
scripts/agent-local.py
agents/pi/Dockerfile
agents/pi/package-lock.json
agents/pi/reliquary-upload.ts
skills/reliquary/SKILL.md
docker-compose.yml
VERSION
```

安装使用客户指定的发布提交；客户未指定时使用默认分支 `main` 的最新提交，并以上述入口文件存在为准。

在 Git 工作副本内执行并记录版本、提交、分支和未提交改动。保留客户的本地改动。

```sh
pwd
git status --short
git branch --show-current
git log -1 --format='%H %s'
cat VERSION
```

若客户下载的是 ZIP，当前安装器需要 Git 提交信息。保留解压目录，另建 Git 工作副本。默认目标已存在时选择空目录。

```sh
mkdir -p "$HOME/Project"
git clone https://github.com/Dikui/Reliquary.git "$HOME/Project/Reliquary"
cd "$HOME/Project/Reliquary"
```

完成条件为已确定实际源码目录、当前提交及安装入口。缺少入口文件时先取得含该实现的版本。

需要群聊入口时，另核对 `agents/chat/Dockerfile` 和 `agents/chat/package-lock.json`。

## 2. 检查基础软件

当前支持 macOS 和 Linux。检查操作系统、Python、Git、Docker 及 Compose。

```sh
uname -s
python3 --version
git --version
docker context show
docker info --format '{{.ServerVersion}}'
docker compose version
docker compose config --help
```

Python 需要 3.9+，Compose 需要支持 `--no-env-resolution`、`--no-path-resolution` 和 JSON 输出。Docker 引擎必须运行，当前用户必须能操作它。

缺少软件时，Agent 按客户系统使用已有包管理器或官方安装程序补齐。需要管理员密码或桌面初始化时交给客户完成，随后重新检查。Docker Desktop 自带 Compose。Linux 使用官方对应发行版的安装步骤。[Python](https://www.python.org/downloads/)、[Git](https://git-scm.com/downloads)、[Docker 安装](https://docs.docker.com/engine/install/)、[Compose 安装](https://docs.docker.com/compose/install/)

Docker 构建会下载 Go、Node 和锁定依赖；配置本地 Pi 时下载锁定的 Pi 与 MCP 依赖，需要群聊入口时再下载群聊 Agent 依赖。宿主机无需另装这些开发依赖。检查网络能访问仓库、镜像与包源、所选模型服务，需要群聊时再检查群聊。估算空间和下载量，明显超出客户预算时先报告。

完成条件为上述命令成功，Compose 参数满足要求。Windows 原生安装尚无经过验收的路径，应向客户说明当前支持范围。

## 3. 选择运行位置并安装

默认源码在 `~/Project/Reliquary`，运行根目录在 `~/Reliquary`。运行根目录必须位于源码目录之外。选择其他目录后，将本文所有命令中的 `$HOME/Reliquary` 替换为实际运行根目录。

先检查目标目录、已有 Docker Compose 项目和 80、8080、6379 端口。若源码有 `.deploy.env`，私下读取其中 `RELIQUARY_DEPLOY_DIR` 识别已有部署。

```sh
docker compose ls
docker ps --format '{{.Names}}\t{{.Ports}}'
```

新机器从源码根目录执行。

```sh
python3 scripts/install-local.py --install --root "$HOME/Reliquary"
```

同机另装一套时，给出独立 `--root`、`--project-name`、`--app-port`、`--frontend-port`。如需更换 Redis 端口，复制 `.env.example` 到权限为 `0600` 的本机临时文件，通过 `--env-file` 传入。该模式会保留文件内凭据，必须同时生成独立随机数据库密码和恰好 32 字节的 `SYSTEM_AES_KEY`。按[本地部署指南](local-deployment.md#客户全新安装)处理，沿用原回环绑定。

安装器会构建后端和网页镜像、初始化独立数据库、启动 PostgreSQL、Redis、Neo4j、后端和网页，随后显示数据外流说明并询问“是否配置本地 Pi？[Y/n]”，默认选择是。协助安装的 Agent 选择是前，先按第 4 步说明外流范围并取得客户同意；尚未准备好时跳过，继续完成本体检查和模型准备，再按第 5 步配置。跳过或非交互运行会打印稍后执行的 `pi-configure` 命令。Pi 失败不影响已完成的本体安装。

运行文件如下；本地 Pi 的文件在 `pi-configure` 时生成，群聊的文件在第 6 步首次 `configure` 时生成。

| 位置 | 内容 |
| --- | --- |
| `deploy/.env` | 端口、数据库凭据、加密密钥与 ChatGPT 回调配置 |
| `deploy/docker-compose.yml` | 基础服务运行配置 |
| `scripts/agent-local.py` | 已安装的 Agent 配置和运维入口 |
| `scripts/pi` | 本地 Pi 容器启动器 |
| `agents/pi/config.json`、`agents/pi/pi.env` | 本地 Pi 镜像、模型与工作目录配置、Reliquary API Key 和代理 |
| `agents/pi/data/agent/` | settings.json、mcp.json、models.json、auth.json、APPEND_SYSTEM.md（入口约束加使用规则全文）和 sessions/ |
| `workspace/` | 本地 Pi 默认工作文件夹，只读挂载到容器 `/workspace` |
| `agents/chat/compose.yaml` | Agent 容器配置（configure 生成） |
| `agents/chat/agent.env` | 客户模型与群聊配置（configure 生成） |
| `agents/chat/data/` | Agent SQLite、Pi 会话及恢复状态（configure 生成） |
| `data/files/` | 上传资源，普通正文保存在 PostgreSQL 数据卷 |

安装日志出现基础服务已启动后，检查基础容器状态及后端、网页代理健康接口。以下命令从实际运行配置读取端口。记录输出的 `WEB_BASE` 和 `API_BASE`，后续网页操作均使用这两个地址。

```sh
docker compose --project-directory "$HOME/Reliquary/deploy" ps
python3 - <<'PY'
from pathlib import Path
import runpy
import urllib.request
root = Path.home() / 'Reliquary'  # 自定义目录时替换为实际运行根目录。
read_env = runpy.run_path(str(root / 'scripts/agent-local.py'))['read_env']
config = read_env(root / 'deploy/.env')
web_base = 'http://127.0.0.1:' + config.get('FRONTEND_PORT', '80')
api_base = 'http://127.0.0.1:' + config.get('APP_PORT', '8080')
print('WEB_BASE=' + web_base, 'API_BASE=' + api_base, sep='\n')
http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
for url in (api_base + '/health', web_base + '/health'):
    with http.open(url, timeout=10) as response:
        print(url, response.status, response.read().decode())
PY
```

完成条件为基础容器运行正常，两个健康接口成功，浏览器能打开本机网页。

### 带数据包安装

仅用于全新安装。`--data` 接受普通备份和交付包。对外交付建议在支持此功能的版本上运行 `reliquary-admin backup <交付目录> --delivery`，提供成功生成的单份 `delivery-*` 目录。交付包清除账号、Key、访问授权、操作日志和模型调用历史。清理在临时克隆库中执行，交付方正式库不受影响。使用干净的 Git 工作副本，并保留从备份提交到发布提交的 Git 历史。交付前确认所选模型和解析器需要哪些客户凭据，并准备好所需的本地模型。[交付包清理范围](personal-use.md#四服务备份恢复与演练)

```sh
python3 scripts/install-local.py --install --root "$HOME/Reliquary" --data "/交付目录/delivery-时间"
```

1. 第一次执行会核对清单、文件校验值和 Git 提交，构建镜像，只启动 PostgreSQL、Redis 和 Neo4j。恢复要求新数据库和两个空存储目录。导入和核对后启动网页，任务保持暂停，`deploy/customer-install.json` 显示 `data_restored`。终端用中文提示打开设置页和续装步骤，完整恢复清单保存在运行目录的 `logs/data-install.log`。
2. Agent 打开命令给出的地址，请客户在「设置 → 模型设置」登录 ChatGPT。使用普通备份时，重新登录原模型使用的账号，刷新并测试模型。使用交付包时，逐个编辑库当前选用的 ChatGPT 聊天模型，选择客户的账号和该账号可用的模型，保存并测试。原模型 ID 会保留，库无需重新选档。若没有可供 Pi 使用的聊天模型，在模型设置中新建聊天模型，来源选择 ChatGPT，再选客户的账号和模型，保存并测试。若清单另有必需凭据，补齐并测试相应服务。
3. 从同一源码提交重跑同一条命令，使用同一路径和同一份数据包。脚本继续核对数据、模型与图谱，安装宿主机的定时备份和恢复演练。`deploy/.maintenance-restore.json` 记录 `ready` 后才升级较旧的数据库结构。最后登记运行源码，标记 `base_ready`，在终端用中文提示安装完成并引导配置本地 Pi。执行 `pi-configure` 时可选「ChatGPT 订阅」及已测试的聊天模型，Pi 会申请自己的收录 API Key。

验收时核对 `base_ready`、本节的容器和健康接口检查、定时计划，以及交付方指定的库、资料和附件是否可读。导入完成后，以客户库为准，后续执行 `--update`。

失败时终端会显示核对错误、不可用的必需模型或缺少的必需凭据。保留日志、运行目录及原包，修复原因后执行脚本打印的命令。若 PostgreSQL 已写入但恢复核对没有完成，脚本会停止重复导入。保留现场并检查恢复状态和日志，必要时使用独立的空运行目录和 Compose 项目另行恢复。较早且缺少维护暂停门禁的快照，需由交付方先升级来源并重新打包。[备份恢复流程](personal-use.md#四服务备份恢复与演练)

### 已有安装更新

已完成第 3 步的客户从安装时登记的 Git 工作副本更新。退出正在运行的本地 Pi 后执行以下命令。

```sh
git pull
python3 scripts/install-local.py --update --root "$HOME/Reliquary"
```

命令使用当前分支的干净提交，非交互更新基础服务和已配置的 Pi、群聊。必要的数据库迁移会执行，默认不创建备份。客户明确授权本次升级前备份时，加 `--backup`。脚本会补入可安全应用的配置并打印跨过版本的升级说明。更新停止时，根据汇总修正配置或迁移问题后重试。需要退回时，用已有备份恢复到空环境。[已有安装更新](local-deployment.md#已有安装更新)

## 4. 创建资料库并准备对话模型

打开第 3 步实际 `WEB_BASE` 后加 `/platform/settings?section=library` 的地址。在“库”页点击“新建库”，填写客户指定的名称、英文目录名和说明。英文目录名可使用小写字母、数字和单个连字符。至少创建一个实际资料库，记录其 ID 与目录名。

空库可以完成 Pi 或群聊连接验收；没有资料库时，Pi 工具调用检查也可成功，回答应是“无资料库”。普通资料由外部工具通过 HTTP 或 MCP 送入 Markdown，网页创建入口用于资料库。收录资料或使用货架时读取对应说明。[收录接口](ingest-api.md)、[货架说明](product-shelf-query.md)

启用本地 Pi 前，向客户说明并取得同意：每轮完整上下文（问题、工具读到的资料片段、从工作文件夹读取的内容、历史回复）发送给所选模型服务商；ChatGPT 订阅经 Reliquary 后端转发给 OpenAI。资料库正文、文件和数据库不会整体同步出去，模型上下文只包含工具实际读到的内容和对话历史。不同意时跳过 Pi，保留已完成的本体安装。[ADR 0029](adr/0029-recommend-local-pi.md)

需要本地 Pi 或群聊时继续准备模型；两者均跳过时直接交付本体安装结果。群聊另外需要取得第 6 步的数据外流同意。

客户选择以下一种 Agent 模型来源。现有授权或私有环境文件可直接复用，密钥无需粘贴到聊天。

### 客户 API

准备以下参数，在第 5 步本地 Pi 或第 6 步群聊的配置向导中写入。

| 参数 | 要求 |
| --- | --- |
| 协议 | `openai-completions`、`openai-responses`、`anthropic-messages` 之一 |
| API 根地址 | 服务商提供的根地址，例如 OpenAI 为 `https://api.openai.com/v1` |
| 模型名称 | 服务商提供的准确模型 ID，且支持工具调用 |
| API Key | 客户账号下有效密钥 |
| 上下文、输出上限 | 使用该模型实际支持的值，输出上限小于上下文 |

向导默认值是 128000 和 8192，Agent 需核对所选模型规格后决定是否使用。与模型提供方有关的参数以其官方文档为准。

### 客户 ChatGPT 订阅

#### 本地 Pi

本地 Pi 不单独登录，经 Reliquary 后端使用客户在网页登录的 ChatGPT 账号，令牌由后端续期。

1. 打开 `WEB_BASE` 加 `/platform/settings?section=models`，请客户点击 Continue with ChatGPT 完成登录。
2. 第 5 步执行 `pi-configure` 选择 ChatGPT 订阅。已有可用的 ChatGPT 聊天模型时直接选用；没有时，向导用唯一已登录账号从该账号可用模型中创建聊天模型，默认 `gpt-6.2-astra`，Pi 推理强度 `high`。

已登录多个账号时，在网页为 Pi 新建聊天模型后再配置。自动创建的模型只供 Pi 对话，资料后台处理模型另行分配。

#### Reliquary Agent 的群聊入口

在第 6 步填写 Pi 支持的 ChatGPT 模型名称。首次使用时，配置向导会启动独立的 Pi 登录。请在部署电脑上打开终端给出的授权页，完成授权后确认浏览器已返回 `http://localhost:1455/auth/callback`。

Pi 凭据保存到 `agents/chat/data/auth.json`，由 Pi 续期。重新授权使用 `agent-local.py login`。宿主 Codex 登录文件的兼容接法见 [Agent 指南](../agents/chat/README.md#模型与凭据)。

Agent 对话模型与资料后台处理配置分别设置。需要摘要、实体处理时，在网页创建聊天模型并分配给资料库。需要向量检索时配置共享 Embedding，重排按需配置。ChatGPT 接入用于生成，Embedding 与重排使用其支持的其他来源。仅配置入口的 API 参数会接通对话，不会配置资料后台处理模型。

完成条件取决于所选入口。API 模式需准备好参数。本地 Pi 的订阅模式需客户已在网页登录 ChatGPT。群聊入口的订阅模式需选定 Pi 模型名称。实际工具调用在第 5 步或第 6 步验证。

## 5. 配置并验证本地 Pi

已在安装器中完成配置时直接验收。否则在可交互终端执行：

```sh
python3 "$HOME/Reliquary/scripts/agent-local.py" pi-configure
```

首次配置从安装时登记的源码构建 Pi 镜像。若找不到源码，从第 1 步确定的源码根目录执行 `python3 scripts/agent-local.py pi-configure --root "$HOME/Reliquary"`。

Agent 的终端不能逐项应答时，用非交互参数一次给全；给出 `--model-source` 后不再提问，缺少的参数直接报错。安装器在非交互终端会跳过 Pi 并打印这条提示，Agent 取得第 4 步同意后执行。

```sh
# ChatGPT 订阅：客户先在网页登录；没有聊天模型时自动创建，默认 gpt-6.2-astra，可用 --model 指定
python3 "$HOME/Reliquary/scripts/agent-local.py" pi-configure --model-source chatgpt

# 客户 API：Key 只从环境变量读取，由客户在自己的终端或私有文件中设置，不经过对话
RELIQUARY_PI_MODEL_API_KEY=... python3 "$HOME/Reliquary/scripts/agent-local.py" pi-configure \
  --model-source api --model-api openai-responses --model-base-url https://api.openai.com/v1 --model 模型名称 \
  --context-window 128000 --max-tokens 8192
```

可选 `--workspace` 指定只读工作文件夹（默认沿用已有设置或 `~/Reliquary/workspace`），`--proxy` 指定容器可达的代理（省略沿用已有设置，`-` 改为直连）。之后的下列步骤与交互方式相同。

1. 再次核对已取得第 4 步的数据外流同意，按提示选择客户 API 或 ChatGPT 订阅。已有兼容的模型配置时可选择复用，否则填写第 4 步的模型参数。
2. 选择一个 Pi 可读取的工作文件夹，默认 `~/Reliquary/workspace`。只放本次任务所需资料，以只读方式挂载到容器的 `/workspace`。模型仍能通过 read 读到容器内 Pi 自己的凭据文件，不把容器视为凭据隔离边界。
3. 按实际网络填写容器可达的模型代理；直连时留空，已有代理输入 `-` 可改为直连。
4. 向导创建本机 Reliquary API Key，写入第 3 步的配置、系统提示（入口约束加 `rules.md` 使用规则全文）和启动器。内置工具只开放 read，MCP 只连接 Reliquary，另有 `reliquary_upload_workbook` 和 `reliquary_stage_attachment`；不开放命令执行和本地文件修改工具。
5. 等待模型实际调用 `list_libraries`，会消耗少量额度。成功输出包含“本地 Pi 已读到资料库”及回答，不能仅凭进程退出码认定成功。失败时配置保留，修正模型、网络或服务后重跑同一命令。

启动 Pi，发出“列出你能查询的资料库”，核对实际名称与网页一致；退出后继续上次会话，请它根据刚才的列表说明各库用途，未填说明的明确标出。

```sh
"$HOME/Reliquary/scripts/pi"
"$HOME/Reliquary/scripts/pi" -c
```

需要处理文件时，在对话中使用 `/workspace` 下的路径。表格必须通过工作簿上传工具走导入方案：已有已确认方案时传 `plan_key`；没有方案时传 `new_plan_key` 与 `file_kind` 请求待审核建议。原件暂存只取得附件 ID，再由 MCP 提议归类；上传不确认业务变更。安装验收只做上述只读问答，不导入客户表格。

完成条件为模型真实工具调用成功、首次问答与继续会话通过，并记录工作文件夹和模型来源。本地 Pi 会话保存在 `agents/pi/data/agent/`，暂未纳入 `reliquary-admin` 备份；更新命令见[本地 Pi 运行与更新](local-deployment.md#本地-pi-运行与更新)。客户不需要群聊时，跳过第 6 步。

## 6. 接入群聊

按[共享聊天服务](../agents/chat/README.md)准备运行目录中的本机平台模块、依赖锁、所需凭据和预期入口名单，再从源码运行 `python3 scripts/agent-local.py configure --root "$HOME/Reliquary"`。已有部署升级使用 `update`。检查两个或多个入口时，各自在原群引用更新前的机器人答复并提问。

## 7. 失败恢复与再次启动

| 现象 | Agent 下一步 |
| --- | --- |
| 下载、构建或首次启动失败 | 保留本次配置和数据，修正网络、磁盘或 Docker 后重跑相同安装命令 |
| 提示已有运行目录或 Compose 项目 | 检查 `deploy/customer-install.json` 和资源归属。同次安装继续，新装选择独立目录、项目名与端口 |
| 基础安装完成后提示配置 Pi 或群聊 | 本体已装好；本地 Pi 按第 4 至 5 步，需要群聊时继续第 6 步。需要复启基础容器时从运行 `deploy/` 执行 `docker compose up -d --no-build --pull never --wait` |
| 本地 Pi 配置或调用检查失败 | 保留 `agents/pi/` 与工作文件夹，修正模型、网络或服务后重新执行 `pi-configure`；更新失败按本地部署指南处理 |
| 模型或群聊身份检查失败 | `configure` 已保存配置。修正实际错误后重试 `check`，成功后执行 `start` |
| 已运行时需要改配置 | 保留原状态和会话，执行 `stop`，等任务结束后重新 `configure` |
| 提示现有会话绑定其他群或库 | 先核对输入。确需换范围时使用独立安装，或按[状态迁移](../agents/chat/README.md#迁移旧状态)处理 |
| 状态健康但群里没有回复 | 核对目标群、真实 @、机器人成员资格、Tenant Key、事件发布状态及平台权限，检查脱敏日志 |
| ChatGPT 回调失败或显示需登录 | 检查实际回调地址、浏览器所在电脑和网络，对需要重登的账号完成授权后重试 |
| 订阅额度暂停 | 保留 Pi 账号与会话状态，查看 ChatGPT 账号用量，按提示恢复 |

```sh
python3 "$HOME/Reliquary/scripts/agent-local.py" check
python3 "$HOME/Reliquary/scripts/agent-local.py" start
python3 "$HOME/Reliquary/scripts/agent-local.py" status
python3 "$HOME/Reliquary/scripts/agent-local.py" stop
docker compose --project-directory "$HOME/Reliquary/agents/chat" logs --tail 50 agent
docker compose --project-directory "$HOME/Reliquary/deploy" logs --tail 50 app
```

上述 `check`、`start`、`status`、`stop` 仅用于群聊，`check` 会调用模型。按当前状态选择命令，停止仅用于客户要求停机或修改配置。`reliquary-admin backup` 已把 `agents/chat/data/`（含 SQLite WAL 与群聊使用的 Pi 会话，不含凭据）与数据库放在同一备份包；恢复后需在运行目录补齐平台凭据并重新配置模型。本地 Pi 使用 `scripts/pi` 启动、`-c` 继续，会话暂未纳入备份。故障排查以保留状态并修正原因为先。备份、数据清理和已有环境升级按各自授权与[维护流程](local-deployment.md#日常维护与升级)执行。

客户需要 macOS 登录后自动恢复基础服务及已启用的群聊时，从源码执行下列命令。本地 Pi 是按需启动的终端会话，不随之自动打开。已存在启动项时先核对并复用。Linux 检查 Docker 已配置开机启动，基础服务与群聊容器沿用 `restart: unless-stopped`。

```sh
python3 scripts/install-local.py --root "$HOME/Reliquary" --install-autostart
```

## 8. 交付结果

向客户报告实际结果，保留以下信息，满足验收条件后停止。

- 源码目录、版本、提交和运行根目录。
- 网页地址、基础服务状态、本地 Pi 是否配置；需要群聊时报告其健康状态，以及是否配置自动启动。
- 所选模型来源及模型名、资料库名称、Pi 只读工作文件夹；需要群聊时报告目标群名称。
- Pi 工具调用、首次回复、继续会话的验收结果，以及客户对数据外流的同意或跳过决定；群聊按实际需要报告消息验收。资料检索示例若执行，记录其库与资料 ID。
- 配置、数据、会话的位置，以及 `scripts/pi`、`scripts/pi -c` 和第 7 步中适用的日常命令；说明本地 Pi 会话暂未纳入备份。故障或待客户完成的步骤写明准确位置与原因。
- 实际新增的资料、API Key、应用和权限范围，报告隐藏凭据值。

既有验收覆盖 macOS 全新安装、真实 API、复用已有 ChatGPT 授权和群聊多轮。在客户环境分别验证所选入口的实际会话、首次订阅授权、Linux 宿主和整机重启恢复。仅脚本或容器检查通过时按实际范围报告。
