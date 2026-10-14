# Reliquary 开发指南

## 快速开发模式（推荐）

如果你需要频繁修改 `app` 或 `frontend` 代码，**不需要每次都重新构建 Docker 镜像**，可以使用本地开发模式。

### 方式一：使用 Make 命令（推荐）

#### 1. 启动基础设施服务

```bash
make dev-start
```

这将启动以下服务的 Docker 容器：

- PostgreSQL（数据库）
- Redis（任务唤醒）
- Neo4j Community 5.26.12（图谱查询副本）

#### 2. 启动后端应用（新终端）

```bash
make dev-app
```

这将在本地直接运行 Go 应用，修改代码后 Ctrl+C 停止，重新运行即可。

#### 3. 启动前端（新终端）

```bash
make dev-frontend
```

这将启动 Vite 开发服务器，支持热重载，修改代码后自动刷新。

#### 4. 查看服务状态

```bash
make dev-status
```

#### 5. 停止所有服务

```bash
make dev-stop
```

### 方式二：使用脚本命令

如果你更喜欢直接使用脚本：

```bash
# 启动基础设施
./scripts/dev.sh start

# 启动后端（新终端）
./scripts/dev.sh app

# 启动前端（新终端）
./scripts/dev.sh frontend

# 查看日志
./scripts/dev.sh logs

# 停止所有服务
./scripts/dev.sh stop
```

## PostgreSQL 集成测试

`go test ./...` 默认运行单元测试。覆盖 JSONB、ILIKE、行锁、NULLS LAST、pg_search 或 pgvector 的测试使用 `internal/testutil/pgtest`；未设置 `RELIQUARY_TEST_PG_DSN` 时明确跳过，设置后连接或迁移失败会使测试失败。

使用带 vector、pg_search、pg_trgm 扩展的 ParadeDB 测试服务，例如本机的 v0.22.2-pg17：

```bash
export RELIQUARY_TEST_PG_DSN='postgres://reliquary:reliquary_test@127.0.0.1:55432/reliquary_test?sslmode=disable'
make test-pg
# 等价命令：go test -count=1 -v ./...
```

辅助包为每个测试包创建独立数据库，执行 `migrations/versioned/000000_init.up.sql` 基线及后续 up 迁移，再为各测试克隆独立数据库。连接用户需要 `CREATEDB` 权限与安装迁移所需扩展的权限。测试只清理自己生成的 `reliquary_pgtest_*` 数据库，不修改连接串指定的数据库。

2026-10-07 的基线由旧 `000000` 至 `000098` 迁移压缩而来。旧数据库不能原地升级，应新建空库或重建 PostgreSQL 数据卷后启动迁移；不能通过强制修改迁移版本或删除版本表来复用旧库。后续迁移从 `000001` 继续编号，操作说明见 [数据库迁移排障](./migration-troubleshooting.md#single-baseline-and-database-rebuild)。

`pgtest.Open(t)` 使用完整迁移后的生产 schema。只验证仓储 SQL 的单元测试可用 `pgtest.OpenFixture(t)` 在克隆数据库的独立 fixture schema 创建最小 PostgreSQL 表；它们仍执行真正的 PostgreSQL SQL，不翻译为 SQLite SQL。使用辅助包的测试包在 `TestMain` 中调用 `os.Exit(pgtest.Run(m))`，确保结束时清理迁移模板。辅助包自测在完整生产 schema 上验证迁移、BM25 检索、向量距离与隔离性。

## 访问地址

### 开发环境

- **前端开发服务器**: http://localhost:5173
- **后端 API**: http://localhost:8080
- **PostgreSQL**: localhost:5432
- **Redis**: localhost:6379
- **Neo4j Bolt**: localhost:7687

## 开发工作流对比

### ❌ 旧方式（慢）

```bash
# 每次修改代码后都需要：
sh scripts/build-images.sh -p      # 重新构建镜像（很慢）
sh scripts/start-all.sh --no-pull  # 重启容器
```

**耗时**：每次修改需要 2-5 分钟

### ✅ 新方式（快）

```bash
# 首次启动（只需要一次）：
make dev-start

# 在另外两个终端分别运行：
make dev-app       # 修改 Go 代码后 Ctrl+C 重启即可（秒级）
make dev-frontend  # 修改前端代码自动热重载（无需重启）
```

**耗时**：
- 首次启动：1-2 分钟
- 后续修改后端：5-10 秒（重启 Go 应用）
- 后续修改前端：实时热重载

## 使用 Air 实现后端热重载（可选）

如果你希望后端代码修改后也能自动重启，可以安装 `air`：

```bash
go install github.com/air-verse/air@latest
```

项目根目录已内置 `.air.toml`，不需要手动创建配置文件。`make dev-app` 会检测本机是否安装了 Air：检测到 Air 时使用热重载模式，未检测到时使用普通 `go run` 模式。

## 其他开发技巧

### 只修改前端

如果只修改前端，只需要：

```bash
cd frontend
npm run dev
```

前端会连接到 http://localhost:8080 的后端 API。

### 只修改后端

如果只修改后端，只需要：

```bash
# 启动基础设施
make dev-start

# 运行后端
make dev-app
```

### 调试模式

#### 后端调试

使用 VS Code 或 GoLand 的调试功能，配置连接到本地运行的 Go 应用。

VS Code 配置示例（`.vscode/launch.json`）：

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch Server",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/cmd/server",
            "env": {
                "DB_HOST": "localhost"
            },
            "args": []
        }
    ]
}
```

#### 前端调试

使用浏览器开发者工具即可，Vite 提供了 source map。

## 生产环境部署

当你完成开发需要部署时，才需要构建镜像：

```bash
# 构建所有镜像
sh scripts/build-images.sh

# 或只构建特定镜像
sh scripts/build-images.sh -p  # 只构建后端
sh scripts/build-images.sh -f  # 只构建前端

# 启动生产环境
sh scripts/start-all.sh
```

## 常见问题

### Q: 启动 dev-app 时报错连接不到数据库

A: 确保先运行了 `make dev-start`，并等待所有服务启动完成（大约 30 秒）。

### Q: 前端访问 API 时报 CORS 错误

A: 检查前端的代理配置，确保 `vite.config.ts` 中配置了正确的代理。

### Q: 收录前如何准备正文？

收录只接受调用方提供的 Markdown 正文和图片。开发构建不需要文件解析工具链。

## 总结

- **日常开发**：使用 `make dev-*` 命令，快速迭代
- **测试集成**：使用 `sh scripts/start-all.sh --no-pull` 测试完整环境
- **生产部署**：使用 `sh scripts/build-images.sh` + `sh scripts/start-all.sh`

## 开发与启动脚本职责

| 脚本 | 用途 | 进程与停止方式 |
| --- | --- | --- |
| `scripts/dev.sh` | 管理开发 Compose 基础设施，分别以前台方式启动本地 Go 后端或 Vite；支持 `.env.local` 与远程基础设施 | 本地应用用 Ctrl+C 停止，开发基础设施用 `make dev-stop` 停止 |
| `scripts/quick-dev.sh` | 交互式调用开发启动流程，可选择把本地后端和前端放到后台，保存日志与 PID | 按脚本输出的 PID 停止所选后台应用；基础设施用 `make dev-stop` 停止 |
| `scripts/start-all.sh` | 管理生产 Compose 容器及本地或远程 Ollama，支持拉取、重启、诊断与容器列表 | `--stop` 停止生产容器及本地 Ollama；Ctrl+C 只退出持续日志 |

三者分别服务开发命令、交互式开发编排及容器部署，均保留。开发和生产模式的架构对比见[快速开发模式](quick-development.md)，通用排障见[常见问题](faq.md)。

## 收录接口与采集表升级

新内容只由外部工具经 HTTP 收录接口或 MCP 送入，网页没有 RSS、网址导入、文件上传或手动录入创建入口。接入总览见 [外部接入](external-integration.md)，契约见 [收录 API](ingest-api.md)。模型、embedding 与正文图片处理继续保留。

开发库升级到 `000005` 前先在相同数据库与 `VAULT_ROOT` 配置下执行 `./reliquary-admin export-sources`，再执行直接迁移；生产使用 `./reliquary-admin upgrade` 自动完成升级前完整备份和导出。导出位于文件库 `.reliquary/exports/`，不含凭据。失败时保持暂停并停止迁移，不能跳过失败删表。

## Redis 集成测试

开发 Compose 同时启动 PostgreSQL 和 Redis，原生后端使用 `REDIS_ADDR=127.0.0.1:6379`。远程开发时指定可达的 `REDIS_ADDR`；内置 Redis 端口默认只绑定本机，也可以通过 SSH 转发使用。测试 Redis 与开发实例分离：

```bash
docker run -d --name reliquary-test-redis -p 127.0.0.1:56379:6379 redis:7-alpine
export RELIQUARY_TEST_REDIS_ADDR=127.0.0.1:56379
make test-integration
```

同时设置上文的 `RELIQUARY_TEST_PG_DSN`。已有 `reliquary-test-redis` 时复用，不重复创建。未提供 Redis 测试地址时，依赖 Redis 的用例会跳过并提示。每个测试按独立 PG 数据库名隔离 Redis 队列，结束时清理自身键，保留容器供后续测试。严禁对共用 Redis 的 DB 0 执行 `FLUSHDB`；清空恢复用例使用独立 DB。前端编译缓存写入 worktree 的 `.cache`，不会修改共享 `node_modules`。

## Neo4j 查询副本与集成测试

`make dev-start` 启动带认证、数据卷的 Neo4j。初始管理员用户名固定为 `neo4j`。
在 `.env` 中设置 `NEO4J_USERNAME=neo4j`、`NEO4J_PASSWORD`，本地 `make dev-app` 会把 Compose 地址转换为宿主机 Bolt 地址；远程开发使用 `DEV_REMOTE_HOST`。`make run` 使用本地 Bolt 地址，密码仍从环境读取。Neo4j 连接失败不会阻止后端启动或 PG 收录。

Compose 固定使用 `neo4j:5.26.12-community` 及镜像摘要。32 GB 服务器的小型部署分配初始堆 512 MB、最大堆 1 GB、页缓存 1 GB，容器上限 3 GB，剩余内存留给 PG、后端和系统。数据放在独立 Neo4j 卷中，不进入备份。

创建并保留测试实例，先用 `docker inspect reliquary-test-neo4j` 检查是否已存在：

```bash
docker run -d --name reliquary-test-neo4j \
  -p 127.0.0.1:57687:7687 \
  -e NEO4J_AUTH=neo4j/reliquary_test \
  -e NEO4J_server_memory_heap_initial__size=256m \
  -e NEO4J_server_memory_heap_max__size=512m \
  -e NEO4J_server_memory_pagecache_size=256m \
  neo4j:5.26.12-community@sha256:9f75e8df4325a24f00fdd7a8c0bcce650a58375049b1058e496e8b43d6c36b37
export RELIQUARY_TEST_PG_DSN='postgres://reliquary:reliquary_test@127.0.0.1:55432/reliquary_test?sslmode=disable'
export RELIQUARY_TEST_REDIS_ADDR=127.0.0.1:56379
export RELIQUARY_TEST_NEO4J_URI=bolt://127.0.0.1:57687
export RELIQUARY_TEST_NEO4J_USERNAME=neo4j
export RELIQUARY_TEST_NEO4J_PASSWORD=reliquary_test
# 停机测试会实际停止容器，必须使用任务独占实例。
# 不停止或重启共用测试容器时，不设置 RELIQUARY_TEST_NEO4J_CONTAINER。
# 容器停机用例只可对自己创建的独立实例显式设置该变量。
make test-integration
```

固定测试密码为 `reliquary_test`，不能用于正式部署。没有设置 Neo4j URI 时集成测试跳过并打印配置提示。图谱队列测试专用 Redis DB 9，其中清空测试会执行 FLUSHDB；不要与其他任务共用 DB 9。Neo4j 测试按独立命名空间隔离并清理图数据，容器结束后保留。测试目标按 Go 包串行执行，以隔离全量清空专用数据库的维护用例。停止容器测试需要显式设置上述容器变量，结束后自动恢复容器。

`go run ./cmd/server maintenance graph-check` 执行一次全量核对。
`go run ./cmd/server maintenance graph-rebuild` 清空专用 Neo4j 数据库后从 PG 重建。
也可用 `make graph-check`、`make graph-rebuild`，连接参数和文件库参数与后端相同。
登录用户可调用 `POST /api/v1/maintenance/graph/rebuild`，返回待处理操作 ID，在运行队列查看状态。API Key 无权调用这个维护入口。

### Redis 测试数据库占用

以下占用按全部 Go、Python 和 Shell 测试中的连接选项及清空操作核查。默认 Redis 地址由 `RELIQUARY_TEST_REDIS_ADDR` 提供，数据库编号由各夹具确定。

| DB | 使用位置 | 隔离与清理 |
| --- | --- | --- |
| 0 | `internal/testutil/redistest` 与 `internal/queue/redis_test.go` 的通用夹具 | 队列名 `reliquary:<临时PG库名>:<逻辑队列>`，Redis 键 `asynq:*<临时PG库名>*`；只清理自己的队列与匹配键 |
| 2 | 本次及后续手工后端运行验证，设置 `REDIS_DB=2` | 使用运行数据库名作为队列前缀；结束只清理对应队列和键 |
| 8 | `TestVectorDeferralRecoversWithoutOrdinaryFailures` | 使用临时 PG 库名隔离，夹具按对应 Redis DB 清理 |
| 9 | `internal/graphreplica` 的队列、周期补推和 Redis 丢失测试 | 图谱专用；丢失测试执行 FLUSHDB，不能与其他测试或后端共用。HTTP 图谱夹具也选 9，但连接的是不可达测试端口 |
| 13 | `TestPeriodicSweepRecoversRedisLossWithoutRestart` | 固定保留，测试前、过程中及结束时执行 FLUSHDB |
| 15 | `TestRedisFlushStartupReplaysAllUnfinishedOperations` | 固定保留，测试前、过程中及结束时执行 FLUSHDB |

Python 管理员恢复测试中的 DB 4 仅为环境配置断言，不创建真实 Redis 连接。未显式配置 DB 的 RedisClientOpt 默认使用 0。`REDIS_DB=2` 只影响通过生产 `RedisOptions` 创建的运行客户端，不覆盖上述夹具固定编号。新增执行 FLUSHDB 的测试必须先核查此表及全部连接选项，选用独占编号。完整 Neo4j 维护测试按 Go 包串行执行，避免全量重建清空其他包的测试图。

## 系列联合验证

在设置上面的 PG、Redis、Neo4j 环境变量后执行：

```bash
go build ./...
go vet ./...
GOFLAGS=-p=1 go test -count=1 -timeout=40m ./...
(cd frontend && npm run type-check && npm run build && npm test)
python3 scripts/test-reliquary-admin.py
```

依赖 PG 或 Redis 的用例会使用真实服务；缺环境变量的集成测试明确跳过。容器停机测试还需要显式容器变量，禁止让它停止共用测试服务。维护核对测试按包串行运行。联合验收使用独立 Compose 项目及文件目录，替身模型验证收录、普通和图谱检索、人工动作、调用统计与恢复流程。浏览器巡检独立执行。资料 JSON 的旧 ID 名称边界见 [外部接入](external-integration.md#资料-id-与兼容字段)。
