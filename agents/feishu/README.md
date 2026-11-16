# 飞书知识助手

独立单进程 Agent，通过 Pi 调用 Reliquary。支持查询、比较、澄清、会话恢复、指定答案派生，以及经发起人确认的货架修改。固定库和群由程序校验。完整 Reliquary API Key 仍拥有服务端全部权限。

## 客户安装入口

从仓库根目录执行全套安装，引导会自动构建 Pi 和基础服务。

```sh
python3 scripts/install-local.py --install --root "$HOME/Reliquary"
python3 "$HOME/Reliquary/scripts/agent-local.py" configure
```

支持客户自己的 API 和 Reliquary 网页连接的 ChatGPT 订阅。已有活跃订阅模型直接复用。新账号由客户在浏览器完成授权。运行目录内的 `agent-local.py` 提供 `check`、`start`、`stop` 和 `status`。从源码执行 `update --root /运行根目录` 可更新 Agent 镜像。完整安装说明见[本地部署指南](../../docs/local-deployment.md#客户全新安装)。

## 飞书应用

在飞书开放平台创建企业自建应用，启用机器人，配置事件订阅为长连接，订阅 `im.message.receive_v1`。申请并发布以下权限，再将机器人加入指定群。

- `im:message.group_at_msg:readonly` 接收群内 @ 消息。
- `im:message:send_as_bot` 回复消息。
- `im:message:readonly` 核对原消息和群。
- `im:message.group_msg` 核对发送结果。这是敏感权限，应用可读取所在群的全部消息，建议机器人只加入专用群。

仅将指定群内 @ 机器人的文字交给模型。附件、Excel、解析器与库设置维护尚未接入。

## 构建

在此目录执行。需要 Docker 和 Compose，开发测试需要 Node 26.12.1。

```sh
npm ci
npm run check
npm test
npm run build
docker build -f Dockerfile -t reliquary-feishu:tested .
```

镜像执行编译后的 `dist/main.js`。本地查询入口继续使用 `npm start`。依赖版本由锁文件固定，镜像基础版本及摘要写在 Dockerfile。准备正式发布时记录经过验收的镜像 ID 或 digest。

## 独立运行目录

例如创建 `/srv/reliquary-feishu`，将 `compose.yaml` 放到此目录。源码目录仅用于构建。不要将运行文件写进仓库。

将 `env.example` 的 Compose 变量放入 `.env`，进程变量放入 `agent.env`。文件权限设为 `0600`。创建本地状态目录，并让容器用户 UID/GID 1000 可读写，目录权限 `0700`。容器固定将该目录挂载到 `/data`。

`RELIQUARY_API_URL` 应能从容器访问。已有 Reliquary Compose 服务可通过同一 Docker 网络使用服务名 `app:8080`，或使用已验证可达的宿主机地址。配置的地址还用于历史会话范围校验，换地址前使用下面的迁移步骤。同机回环后端的 Host 限制见文末说明。

```sh
docker compose --project-directory /srv/reliquary-feishu config --quiet
docker compose --project-directory /srv/reliquary-feishu up -d --no-build --pull never --wait
docker compose --project-directory /srv/reliquary-feishu logs --tail 50 agent
docker compose --project-directory /srv/reliquary-feishu exec -T agent node --input-type=module -e \
  "console.log(await (await fetch('http://127.0.0.1:8787/ready')).json())"
docker compose --project-directory /srv/reliquary-feishu stop
```

健康端口仅监听容器回环地址，无需映射端口。`/health` 反映进程存活，`/ready` 需要存储可用、初始恢复完成、飞书连接成功。模型运行中仍可就绪。`lastError` 仅返回安全错误码。短暂断线由 SDK 重连，终止性连接失败会退出，交给 `restart: unless-stopped` 重启。健康状态变为 unhealthy 本身不会触发 Docker 重启。

## 模型与凭据

客户 API 使用 `MODEL_SOURCE=api`，支持 `openai-completions`、`openai-responses` 和 `anthropic-messages`。填写地址、密钥、模型名称、上下文和输出长度。代理地址必须能从容器访问，内部地址加入 `NO_PROXY`。

客户 ChatGPT 订阅使用 `MODEL_SOURCE=chatgpt` 和已在 Reliquary 中创建的 `MODEL_ID`。模型请求经本机 `/api/v1/agent/models/:model_id/responses` 转发，OAuth 令牌由后端加密保存和续期，Agent 通过本机 API Key 认证。订阅暂停或授权失效会返回可处理的错误码。模型账号设置中的现有订阅额度策略同时适用，工具循环中的每次模型请求都会计入请求数。首次客户授权由 Reliquary 网页引导。

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

轮换飞书、模型或 Reliquary 密钥时，更新运行目录的 `agent.env`，执行 `docker compose up -d --force-recreate --no-build --pull never`。保留状态目录。不要将环境内容、`docker inspect` 完整输出或包含密钥的 Compose 展开结果写入日志。开发联调可使用独立测试密钥验证轮换。

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

备份独立于部署。需要备份时先取得本次授权，使用运行目录的 `./reliquary-admin backup`：它会先停止 Agent，把状态目录（不含 `auth.json` 和运行锁）与基础服务数据写入同一备份包，结束后恢复运行；`agent.env` 中的凭据不进入备份包，恢复后用 `configure` 重填。不要将数据回滚与镜像回退混为一谈。已确认的货架业务变更需要单独的新提议处理。

## 恢复与排障

排队任务启动后继续。中断模型轮次保留完成的历史，运行结果不明的提议先核对远端变更集，发送结果不明先核对飞书历史。长期待核对状态会保留，禁止凭猜测重复创建或删除状态。

启动失败时检查配置字段、文件权限、固定库、凭据有效期和机器人群成员资格。确认失败时查看新预览的修订号和冲突。修改过端点时先迁移状态。恢复旧镜像前核对其状态格式兼容性，保留当前状态和已确认记录。

综合验收使用独立 Compose 项目、隔离 Reliquary 数据及指定飞书测试群。正式切换需另外确认，停止旧机器人长连接后再启动新实例，避免同一应用由两个进程接收事件。

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
