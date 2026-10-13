# Reliquary

后台任务与业务数据同存 PostgreSQL，支持事务内入队与崩溃恢复，部署不需要单独的队列服务。详见 [ADR 0010](docs/adr/0010-postgres-task-queue.md)。

[English](README.md)

Reliquary 是个人信息组件，负责文章的收集、存储、去重与检索。一个个人空间只有一个库。外部 AI 通过收录接口与 MCP 进行综合分析、整理和写报告。

源码底座来自 [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0)，上游提交为 `1edcd54b43606d9079bb36650efe3f68707a79ea`。来源、命名与许可说明见 [UPSTREAM.md](UPSTREAM.md)。

## 功能

| 能力 | 当前行为 |
| --- | --- |
| 收录 | 外部工具通过 HTTP 收录接口或 MCP 送入完整内容；Reliquary 不抓取原文 |
| 收录审核 | 送入内容经过收录审核，未通过或缺正文的内容进入待处理，由用户审核、补全或丢弃 |
| 文章处理 | 一次处理调用生成内容类型、统一摘要、实体和用户定义的主题；支持视觉的处理模型可在同次调用中读取正文图片 |
| 处理方式 | 事件、观点、研究全文保存，保留正文、图片与原始文件；教程、案例、参考保存摘要加链接 |
| 文件库 | 审核通过的文章生成带表头的 Markdown 文件，`mine/` 保存用户文档，`reports/` 保存整理结果 |
| 检索 | 全文与向量混合检索，按文章合并结果；可按内容类型、实体、主题、发布时间、星标与来源目录筛选，并折叠相似报道 |
| 用户判断 | 星标、修正内容类型与主题、合并实体、查看操作日志；新建主题可批量归入历史文章摘要 |
| 外部 AI | MCP 支持检索、读取文章、收录、待审区处理、星标、实体与主题查询、保存整理结果 |
| 设置 | 库、模型、文件库状态、操作日志、API 集成与运行队列 |

正文在补全完成或用户接受缺失后定稿。PostgreSQL 保存索引、实体档案、主题、审核状态、用户判断与操作日志。文章表头由数据库单向生成，在 Obsidian 中手动修改表头可能被覆盖。用户文档只读并建立索引。

网页用于管理文章和简单阅读，日常阅读可用 Obsidian 打开文件库。综合分析与写报告由外部 AI 完成。内容只能由外部工具经 HTTP 收录接口或 MCP 送入，原始文件与图片也由外部工具提供。网页阅读时仍显示原网站图片，由浏览器直接加载，后端不下载或代理，缺失材料记录保持不变。RSS 订阅、网址导入、网页上传和手动录入入口已删除，已有文章及其 RSS、网页来源标记仍可读取。统一术语与架构见 [CONTEXT.md](CONTEXT.md) 和 [ADR 0012](docs/adr/0012-ingest-only-no-collection.md)；接口说明由任务 4 编写于 `docs/ingest-api.md`。

## 从源码启动

需要 Git、Node.js/npm、Docker 兼容运行环境（如 OrbStack、Docker Desktop）与 Docker Compose。应用镜像从本仓库构建，尚未发布安装包或公共应用镜像。

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env
# 配置数据库密码、JWT_SECRET、SYSTEM_AES_KEY 与模型服务地址。
./scripts/build-frontend-dist.sh
./reliquary-admin check-deployment
docker compose up -d --wait postgres
./reliquary-admin check-deployment
docker compose up -d --build
```

前端构建脚本在正常安装时通过 `npm ci` 安装依赖。Compose 直接绑定宿主机目录：`RELIQUARY_VAULT_DIR` 默认 `${HOME}/Reliquary/vault`，`RELIQUARY_FILES_DIR` 默认 `${HOME}/Reliquary/files`，`BACKUP_DIR` 默认 `${HOME}/Backups/Reliquary`，均可在 `.env` 修改。`check-deployment` 创建不存在的目录，拒绝不可写、位于源码内或数据与备份目录互相包含的配置。数据库命名卷不变。详见[部署在 Mac mini](docs/personal-use.md#部署在-mac-mini)。

文件库、上传资源、备份目录不要放在“桌面”或“文稿”目录下。

在 Obsidian 打开 `vault/` 这一层，将 `attachments/` 与 `.reliquary/` 排除在搜索之外，不要修改文章表头和 `.reliquary/`。`.obsidian/` 阅读配置随备份保存，核对与索引忽略其内容。

默认网页地址为 `http://localhost`，后端 API 地址为 `http://localhost:8080`。首个用户注册后关闭注册，登录用户与有效 API Key 可访问个人空间全部内容。部署在内网使用。

Apple Silicon 可在 macOS 原生运行 Ollama，通过 Metal 运行模型。生成模型支持 Ollama、OpenAI 兼容接口与 Anthropic 原生接口；嵌入与重排按对应支持的接口配置。设置方法与外部接入见[个人使用指南](docs/personal-use.md)。

## 备份与迁移

升级使用 `./reliquary-admin upgrade`。命令暂停收录并完整备份旧代码、旧镜像、数据库、文件库与资源。迁移 `000005` 删表前，将不含凭据的数据源配置和同步日志导出到文件库 `.reliquary/exports/`。导出失败则不执行迁移并保持暂停。旧版本备份仍可恢复，恢复时也先导出再迁移。

```bash
./reliquary-admin backup /path/to/backups
./reliquary-admin upgrade
# 开发环境直接执行迁移前先运行：
./reliquary-admin export-sources
```

备份保存宿主机文件及 Obsidian 阅读配置，不包含可用凭据。`unpack` 与 `restore` 要求目标宿主机文件库和上传资源目录为空或不存在。恢复后重新填写模型与解析器凭据，步骤见[个人使用指南](docs/personal-use.md)、[迁移规范](docs/database-migrations.md)与[维护数据归属](docs/maintenance-data.md)。

## 开发与验证

```bash
make dev-start
make dev-app       # 在另一终端运行
make dev-frontend  # 在另一终端运行
```

详见[开发指南](./docs/development.md)。Issues 与 specs 使用 `.scratch/<feature-slug>/` 内的本地 Markdown，遵循[任务记录约定](docs/agents/issue-tracker.md)。

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
