# 共享聊天服务

运行目录使用 `$ROOT` 表示。将消息平台的接入代码放在 `$ROOT/integrations/chat/`，该目录独立于 Git 源码和会话数据。配置所选平台前，应向使用者说明群消息、答复和模型上下文的数据外流范围。

## 接入与构建

本机接入目录提供 `main.ts`、其他顶层 TypeScript 模块、`package.json` 和 npm 锁文件。`main.ts` 从 `../service.js` 导入 `runService`，提供一个返回 `MessageChannel[]` 的配置函数。每个入口声明名称、租户与群的绑定、独立的数据目录和消息传输实现。选定的数据库及会话路径在后续更新时沿用。平台 SDK 由本机接入目录的锁文件管理。

在独立文件 `$ROOT/integrations/chat.json` 中写入必须在线的入口名称，例如 `["alpha", "beta"]`，名称与健康端点返回的渠道键一致。将平台凭据保存在运行目录的 `agent.env` 中。新安装使用 `agents/chat/`，已有安装沿用 `agents/feishu/`。初始化前可以先创建该环境文件。配置向导保留其中的平台设置。

从干净的源码检出执行：

```sh
python3 scripts/agent-local.py update --root "$ROOT"
python3 scripts/agent-local.py status --root "$ROOT"
```

更新使用 Docker BuildKit 将本机模块作为独立构建上下文，仅传入接入源文件及依赖清单。切换后核对实际容器的 `/data` 挂载、服务就绪状态和每个预期入口的连接状态。验收失败时恢复前一镜像并重新核对。已停用的服务在下次启动时切换并验收。缺少接入源文件或预期入口清单时，构建会在切换前停止。旧版内置平台适配器需先准备为本机接入模块，再升级。

群内验收时，原发起人在各群引用一条更新前的答复并提及机器人。核对新答复已投递、父消息指向被引用的答复、会话标识及 Pi 历史文件与旧记录一致。

## 模型与凭据

客户 API 使用 `MODEL_SOURCE=api`，支持 `openai-completions`、`openai-responses` 和 `anthropic-messages`。填写地址、密钥、模型名称、上下文和输出长度。代理地址必须能从容器访问，内部地址加入 `NO_PROXY`。

客户 ChatGPT 订阅使用 `MODEL_SOURCE=chatgpt`，默认模型为 `gpt-6.1-sol`。Pi 直接请求 ChatGPT，OAuth 凭据保存在 Agent 的 `data/auth.json`，由 Pi 续期。Reliquary API Key 用于知识库工具调用。重新登录使用以下命令，在输出的授权页完成账号登录。浏览器回调使用本机端口 1455。

```sh
python3 /运行根目录/scripts/agent-local.py login
```

以下为开发者本机复用 Codex 登录文件的兼容运行方式，不用于客户安装。`MODEL_API=openai-codex-responses` 必须显式填写；缺少 `MODEL_API` 时 Agent 拒绝启动，不会回落到 Codex 登录。

使用本机 Codex 登录时，增加独立的 Compose 覆盖文件 `compose.codex.yaml`，并在 `.env` 写入宿主登录文件绝对路径 `CODEX_AUTH_SOURCE`。

```yaml
services:
  agent:
    volumes:
      - type: bind
        source: ${CODEX_AUTH_SOURCE:?Set the Codex auth.json path}
        target: /run/codex/auth.json
        read_only: true
        bind:
          create_host_path: false
```

启动时增加 `-f compose.yaml -f compose.codex.yaml`。Agent 只读使用访问令牌，由宿主 Codex 续期。容器用户需要读取权限。宿主以替换文件方式更新登录缓存后，重建 Agent 容器以挂载新文件。

轮换平台、模型或 Reliquary 密钥时，更新运行目录的 `agent.env`，执行 `docker compose up -d --force-recreate --no-build --pull never`。保留状态目录。不要将环境内容、`docker inspect` 完整输出或包含密钥的 Compose 展开结果写入日志。开发联调可使用独立测试密钥验证轮换。

## 使用

- 独立 @机器人提问会创建会话。
- 回复机器人消息并 @ 会继续该会话。
- 回复完整答案并输入 `分支：问题` 会从指定答案派生。
- 修改请求先产生待确认变更集，程序展示差异和影响。
- 发起人回复完整预览并 @，发送 `确认 口令` 或 `取消 口令`。
- 口令有效期 10 分钟。修订、到期和状态变化后重新核对预览。

## 迁移旧状态

迁移会复制旧状态到新目录，源目录保留。先停止使用源目录的全部旧进程。迁移程序会检查本地锁、旧版 PID、群范围、库身份、会话引用和端点。新目录必须不存在。

示例使用已构建产物在宿主离线执行。`target.env` 包含新的 Reliquary 端点、同一组库、同一飞书范围，`AGENT_DATA_DIR` 指向新的宿主目录。

```sh
node --use-env-proxy --env-file=/absolute/target.env dist/migrate.js \
  --from /absolute/old-state \
  --from-endpoint http://127.0.0.1:8080 \
  --runtime-root /data
```

迁移调整 Pi 头部路径和端点标记，消息、答案 ID、分支关系及业务变更集保持原样。待确认提议的口令到期时间置零，启动后重新核验。目标 `agent.env` 中的 `AGENT_DATA_DIR` 使用 `/data`。启动前设置目标文件的 UID/GID，避免容器用户无法读写。迁移失败会移除本次临时目标，源状态保留。

可在 Node 镜像内运行编译后的迁移入口，挂载源、目标父目录及所需只读凭据。旧版状态的 PID 检查应在原宿主完成，避免不同 PID 命名空间造成误判。

状态目录必须使用本地文件系统，并只允许一个 Agent 实例。`runtime-lock.sqlite` 由连接持有排他事务，进程退出自动释放。保留整个状态目录，包括 SQLite 的 WAL 文件、Pi `sessions/` 和 Pi 自行管理的 `auth.json`。不要只复制运行中的主数据库文件，也不要删除运行中的锁文件。

备份独立于部署。需要备份时先取得本次授权，使用运行目录的 `./reliquary-admin backup`：它会先停止 Agent，把状态目录（不含 `auth.json` 和运行锁）与基础服务数据写入同一备份包，结束后恢复运行；`agent.env` 中的凭据不进入备份包，恢复后补齐平台凭据并用 `configure` 配置模型。不要将数据回滚与镜像回退混为一谈。已确认的货架业务变更需要单独的新提议处理。

## 恢复与排障

排队任务启动后继续。中断模型轮次保留完成的历史，运行结果不明的提议先核对远端变更集，发送结果不明先核对平台历史。长期待核对状态会保留，禁止凭猜测重复创建或删除状态。

启动失败时检查配置字段、文件权限、固定库、凭据有效期和机器人群成员资格。确认失败时查看新预览的修订号和冲突。修改过端点时先迁移状态。恢复旧镜像前核对其状态格式兼容性，保留当前状态和已确认记录。

综合验收使用独立 Compose 项目、隔离 Reliquary 数据及各平台指定的测试群。正式切换需另外确认，停止旧机器人长连接后再启动新实例，避免同一应用由两个进程接收事件。

### 同机回环服务的容器连接

MCP 会检查回环服务请求的 Host。当前后端会拒绝通过 `host.docker.internal` 访问回环 MCP 的请求。Linux 可在独立覆盖文件配置 `network_mode: host`，使用原 `http://127.0.0.1:端口`。Docker Desktop 需要先启用其宿主网络能力，具体支持由本机 Docker 决定。容器和宿主不能同时占用健康端口 8787，切换前先停止旧 Agent。

当迁移目标端点仅在容器内可解析时，增加 `--target-check-endpoint` 指定迁移所在环境可达的同一后端地址。程序会核对双方库 ID 和目录身份，目标会话保存的仍是目标配置端点。

```sh
node --env-file=/absolute/target.env dist/migrate.js \
  --from /absolute/old-state \
  --from-endpoint http://127.0.0.1:8080 \
  --target-check-endpoint http://127.0.0.1:8080 \
  --runtime-root /data
```
