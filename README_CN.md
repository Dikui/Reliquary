# Reliquary

后台任务与业务数据同存 PostgreSQL，支持事务内入队与崩溃恢复，部署不需要单独的队列服务。详见 [ADR 0010](docs/adr/0010-postgres-task-queue.md)。

[English](README.md)

Reliquary 是个人信息组件，负责文章的收集、存储、去重与检索。一个个人空间只有一个知识库。外部 AI 通过收录接口与 MCP 进行综合分析、整理和写报告。

源码底座来自 [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0)，上游提交为 `1edcd54b43606d9079bb36650efe3f68707a79ea`。来源、命名与许可说明见 [UPSTREAM.md](UPSTREAM.md)。

## 功能

| 能力 | 当前行为 |
| --- | --- |
| 收集 | 全局 RSS 订阅、上传文件、按网址收录，以及供外部采集器调用的收录接口 |
| 收录审核 | 订阅内容经过收录审核，未通过的内容保存在待审区，由用户审核通过或丢弃 |
| 文章处理 | 一次处理调用生成内容类型、统一摘要、实体和用户定义的主题；支持视觉的处理模型可在同次调用中读取正文图片 |
| 处理方式 | 事件、观点、研究全文保存，保留正文、图片与原始文件；教程、案例、参考保存摘要加链接 |
| 文件库 | 审核通过的文章生成带表头的 Markdown 文件，`mine/` 保存用户文档，`reports/` 保存整理结果 |
| 检索 | 全文与向量混合检索，按文章合并结果；可按内容类型、实体、主题、发布时间、星标与来源目录筛选，并折叠相似报道 |
| 用户判断 | 星标、修正内容类型与主题、合并实体、查看操作日志；新建主题可批量归入历史文章摘要 |
| 外部 AI | MCP 支持检索、读取文章、收录、待审区处理、星标、实体与主题查询、保存整理结果 |
| 设置 | 知识库、订阅源、模型、文件库状态、操作日志、API 集成与运行队列 |

正文首次写入后保持不变。PostgreSQL 保存索引、实体档案、主题、审核状态、用户判断与操作日志。文章表头由数据库单向生成，在 Obsidian 中手动修改表头可能被覆盖。用户文档只读并建立索引。

网页用于管理文章和简单阅读，日常阅读可用 Obsidian 打开文件库。综合分析与写报告由外部 AI 完成。统一术语与架构见 [CONTEXT.md](CONTEXT.md) 和 [ADR 0009](docs/adr/0009-kb-collect-store-retrieve.md)。

## 从源码启动

需要 Git、Node.js/npm、Docker 与 Docker Compose。应用镜像从本仓库构建，尚未发布安装包或公共应用镜像。

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env
# 配置数据库密码、JWT_SECRET、SYSTEM_AES_KEY 与模型服务地址。
./scripts/build_frontend_dist.sh
docker compose up -d --build
```

前端构建脚本在正常安装时通过 `npm ci` 安装依赖。启动前需配置可直接阅读的文件库目录及写入权限，见[部署在 Mac mini](docs/personal-use.md#部署在-mac-mini)。

默认网页地址为 `http://localhost`，后端 API 地址为 `http://localhost:8080`。首个用户注册后关闭注册，登录用户与有效 API Key 可访问个人空间全部内容。部署在内网使用。

Apple Silicon 可在 macOS 原生运行 Ollama，通过 Metal 运行模型。生成模型支持 Ollama、OpenAI 兼容接口与 Anthropic 原生接口；嵌入与重排按对应支持的接口配置。设置方法与外部接入见[个人使用指南](docs/personal-use.md)。

## 备份与迁移

暂停应用写入与文件编辑后，一起备份 PostgreSQL、文件库和上传资源。部署配置与加密密钥需另行复制。[迁移指南](docs/personal-use.md#迁移到另一台-mac-mini)说明新 Mac mini 上的恢复步骤、文章与检索核对，以及没有数据库备份时从文件库重建的方法。

```bash
docker compose stop app frontend
./scripts/backup.sh /path/to/new-backup
# 在目标机器先启动 PostgreSQL，并创建处于停止状态的 app 容器。
./scripts/restore.sh /path/to/new-backup
```

两个脚本都接收备份目录，并使用 `COMPOSE_FILE` 与 `COMPOSE_PROJECT_NAME`。运行 `--help` 查看宿主机目录覆盖选项及目标为空的要求。

旧 WeKnora 或旧 Reliquary 数据库需重新安装。本节迁移只用于当前结构在两台机器之间的搬迁，不升级旧结构。参见[数据库基线重建说明](docs/migration-troubleshooting.md#single-baseline-and-database-rebuild)。

## 开发与验证

```bash
make dev-start
make dev-app       # 在另一终端运行
make dev-frontend  # 在另一终端运行
```

详见[开发指南](docs/开发指南.md)。Issues 与 specs 使用 `.scratch/<feature-slug>/` 内的本地 Markdown，遵循[任务记录约定](docs/agents/issue-tracker.md)。

```bash
go build ./...
go vet ./...
go test -count=1 ./...
cd frontend
npm run type-check
npm run build
npm test
```

数据库集成测试通过 `RELIQUARY_TEST_PG_DSN` 指定 PostgreSQL 测试库。修改的 Go 文件使用 `gofmt` 格式化；基线或环境失败需记录对应命令。

## 许可证

[MIT](LICENSE)。保留仓库内的上游署名和第三方许可。
