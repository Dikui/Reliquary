# Reliquary

四个核心服务为 PostgreSQL、Redis、Neo4j 与 Reliquary。PG 保存业务正本和待处理操作，Redis 调度任务，Neo4j 提供可重建的图谱副本。详见 [ADR 0014](docs/adr/0014-internal-knowledge-base.md)。

[English](README.md)

Reliquary 是个人 AI 工作平台的内部资料库，负责资料的收录、存储、去重、普通检索与知识图谱。一个个人空间初始化五个系统库，可按模板新建库。外部 AI 通过 HTTP 与 MCP 进行检索、综合分析和写报告。接入 Grok Bot 等工具从 [外部接入总览](docs/external-integration.md) 开始。

源码底座来自 [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0)，上游提交为 `1edcd54b43606d9079bb36650efe3f68707a79ea`。来源、命名与许可说明见 [UPSTREAM.md](UPSTREAM.md)。

## 功能

| 能力 | 当前行为 |
| --- | --- |
| 收录 | 外部工具通过 HTTP 收录接口或 MCP 送入完整内容；Reliquary 不抓取原文 |
| 收录审核 | 送入内容经过收录审核，未通过或缺正文的内容进入待处理，由用户审核、补全或丢弃 |
| 资料处理 | 联合输出内容类型、摘要、实体、主题、关系及图片说明；长文分段并保留检查点，AI 报告免审核与内容处理 |
| 所有内容类型 | 保存 Markdown 全文和图片 |
| 文件库 | 每库一个英文目录：`institutional/`、`opinions/`、`references/`、`mine/`、`ai-reports/` 或自建目录，统一表头并保存全文；图片位于 `attachments/<资料ID>/` |
| 检索 | 全文与向量混合检索，按资料合并结果；可按内容类型、实体、主题、发布时间、星标与库筛选，并折叠相似报道 |
| 用户判断 | 星标、修正内容类型与主题、合并实体、删除关系、合并关系类型、查看操作日志；新建主题可批量归入历史资料摘要 |
| 外部 AI | MCP 支持检索、读取文章、收录、待审区处理、星标、实体与主题查询、写入 AI 报告 |
| 图谱检索 | 按库查询实体 1 至 3 跳关系或两点路径，返回来源与正文位置，不调用模型 |
| 测评与统计 | 题集、运行、五项指标、逐题比较及模型调用价格快照 |
| 设置 | 库、模型、文件库状态、操作日志、API 集成与运行队列 |

正文在补全完成或用户接受缺失后定稿。PostgreSQL 保存索引、实体档案、主题、审核状态、用户判断与操作日志。所有库的表头由数据库单向生成，刷新及重建会覆盖 Obsidian 手改。文件名使用标题短名和 ID 前八位，不设年月子目录。字段及副本登记方式见[维护数据](docs/maintenance-data.md#按库文件与统一表头)。

网页用于管理资料和简单阅读，日常阅读可用 Obsidian 打开文件库。综合分析与写报告由外部 AI 完成。内容只能由外部工具经 HTTP 收录接口或 MCP 送入，正文由调用方转换为 Markdown，图片由调用方上传。网页展示已上传的图片，缺少的图片显示缺失提示。RSS 订阅、网址导入、网页上传和手动录入入口已删除，已有文章及其 RSS、网页来源标记仍可读取。统一术语与架构见 [CONTEXT.md](CONTEXT.md) 和 [ADR 0012](docs/adr/0012-ingest-only-no-collection.md)；接口见 [收录](docs/ingest-api.md)、[普通与图谱检索](docs/search-api.md)、[关系](docs/relations-api.md)、[测评](docs/search-evaluation-api.md) 和 [模型调用](docs/model-calls.md)。

## 从源码启动

长期使用建议按[源码与运行分离的部署指南](docs/local-deployment.md)安装，将运行配置和数据放在源码之外。以下命令用于直接从源码目录启动。

需要 Git、Node.js/npm、Docker 兼容运行环境（如 OrbStack、Docker Desktop）与 Docker Compose。应用镜像从本仓库构建，尚未发布安装包或公共应用镜像。

```bash
git clone https://github.com/Dikui/Reliquary.git
cd Reliquary
cp .env.example .env
# 配置数据库密码、SYSTEM_AES_KEY 与模型服务地址。
./scripts/build-frontend-dist.sh
./reliquary-admin check-deployment
docker compose up -d --wait postgres redis neo4j
./reliquary-admin check-deployment
docker compose up -d --build
```

前端构建脚本在正常安装时通过 `npm ci` 安装依赖。Compose 直接绑定宿主机目录：`RELIQUARY_VAULT_DIR` 默认 `${HOME}/Reliquary/vault`，`RELIQUARY_FILES_DIR` 默认 `${HOME}/Reliquary/files`，`BACKUP_DIR` 默认 `${HOME}/Backups/Reliquary`，均可在 `.env` 修改。`check-deployment` 创建不存在的目录，拒绝不可写、位于源码内或数据与备份目录互相包含的配置。数据库命名卷不变。详见[部署在 Mac mini](docs/personal-use.md#部署在-mac-mini)。

文件库、上传资源、备份目录不要放在“桌面”或“文稿”目录下。

在 Obsidian 打开 `vault/` 这一层，将 `attachments/` 与 `.reliquary/` 排除在搜索之外，不要修改资料表头和 `.reliquary/`。`.obsidian/` 阅读配置随备份保存，核对与索引忽略其内容。

默认网页地址为 `http://localhost`，后端 API 地址为 `http://localhost:8080`。没有登录，端口只绑定本机，打开网页即可使用；外部工具通过 API Key 访问。

Apple Silicon 可在 macOS 原生运行 Ollama，通过 Metal 运行模型。生成模型支持 Ollama、OpenAI 兼容接口与 Anthropic 原生接口；嵌入与重排按对应支持的接口配置。设置方法与外部接入见[个人使用指南](docs/personal-use.md)。

## 备份与迁移

升级使用 `./reliquary-admin upgrade`。命令暂停收录并完整备份旧代码、旧镜像、数据库、文件库与资源。迁移 `000005` 删表前，将不含凭据的数据源配置和同步日志导出到文件库 `.reliquary/exports/`。导出失败则不执行迁移并保持暂停。旧版本备份仍可恢复，恢复时也先导出再迁移。

```bash
./reliquary-admin backup /path/to/backups
./reliquary-admin upgrade
# 开发环境直接执行迁移前先运行：
./reliquary-admin export-sources
```

备份保存宿主机文件及 Obsidian 阅读配置，不包含可用凭据。`unpack` 与 `restore` 要求目标宿主机文件库和上传资源目录为空或不存在。恢复后重新填写模型凭据，步骤见[个人使用指南](docs/personal-use.md)、[迁移规范](docs/database-migrations.md)与[维护数据归属](docs/maintenance-data.md)。

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
GOFLAGS=-p=1 go test -count=1 -timeout=40m ./...
cd frontend
npm run type-check
npm run build
npm test
```

集成测试需配置真实 PG、Redis 与 Neo4j，环境变量见 [开发指南](docs/development.md#系列联合验证)。修改的 Go 文件使用 `gofmt` 格式化；基线或环境失败需记录对应命令。

## 许可证

[MIT](LICENSE)。保留仓库内的上游署名和第三方许可。

对外资料 ID 统一为 `entry_id`，列表为 `entry_ids`；MCP 全文读取使用 `get_entry`，报告引用使用 `related_entry_ids`。不保留旧字段或旧工具别名。
