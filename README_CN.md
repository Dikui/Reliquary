<div align="center">

# Reliquary

**个人 AI 工作平台的本机单用户资料库。**

外部 AI 工具通过 HTTP 或 MCP 送入完整 Markdown，Reliquary 负责存储、索引、关联与检索。

[![App](https://github.com/Dikui/Reliquary/actions/workflows/app.yml/badge.svg)](https://github.com/Dikui/Reliquary/actions/workflows/app.yml)
[![Frontend](https://github.com/Dikui/Reliquary/actions/workflows/frontend.yml/badge.svg)](https://github.com/Dikui/Reliquary/actions/workflows/frontend.yml)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[English](README.md) | 简体中文

</div>

## 目录

- [简介](#简介)
- [功能](#功能)
- [架构](#架构)
- [快速开始](#快速开始)
- [使用](#使用)
- [备份与升级](#备份与升级)
- [开发](#开发)
- [文档](#文档)
- [许可证](#许可证)

## 简介

Reliquary 是个人 AI 工作平台的内部资料库。它不抓取任何内容：Grok Bot、外部 Agent、采集程序负责取得原文、转换为 Markdown 并上传图片后提交；Reliquary 负责收录审核、存储、去重、抽取实体与关系，并向这些工具提供普通检索和图谱检索。

个人空间初始化五个系统库，也可按模板新建库：

| 目录 | 收录内容 |
| --- | --- |
| `official/` | 机构、公司、媒体发布的新闻、公告、财报、时效研报、政策 |
| `media/` | 外部人物的评论、访谈、观点文章 |
| `references/` | 论文、书籍、标准、官方文档、教程 |
| `personal/` | 本人的工作资料、经验、SOP、笔记 |
| `ai-output/` | 外部 AI 写出的综合分析和报告 |

网页用于管理资料和简单阅读，日常阅读可用 [Obsidian](https://obsidian.md) 打开文件库。

## 功能

- **只收录** —— 完整内容经带认证的 HTTP 收录接口或 MCP 送入。未通过审核或缺正文的内容进入待处理，由你审核、补全或丢弃。
- **联合处理** —— 一次输出摘要、内容类型、实体、主题、关系及图片说明；长文分段处理并保留检查点。
- **全文保存** —— 每条资料保存 Markdown 全文和图片，按库写入文件库，表头由数据库生成。
- **混合检索** —— 全文与向量混合，按资料合并结果；可按内容类型、实体、主题、发布时间、星标与库筛选，并折叠相似报道。
- **图谱检索** —— 查询实体 1 至 3 跳关系或两点路径，返回来源资料与正文位置，不调用模型。
- **用户判断** —— 星标、修正内容类型与主题、合并实体与关系类型、查看操作日志。
- **MCP 工具** —— 检索、读取资料、收录、处理待审、星标、查询实体与主题、写入 AI产出。
- **测评** —— 题集、五项检索指标、运行比较及模型调用价格快照。
- **模型** —— 生成模型支持 Ollama（Apple Silicon 上原生 Metal）、OpenAI 兼容接口与 Anthropic 原生接口；嵌入与重排按对应支持的接口配置。

## 架构

| 服务 | 职责 |
| --- | --- |
| PostgreSQL | 业务正本：资料、索引、审核状态、待处理操作 |
| Redis | 任务调度 |
| Neo4j | 可重建的图谱副本 |
| Reliquary | Go 后端（HTTP + MCP）与 Vue 3 前端 |

领域模型见 [ADR 0014](docs/adr/0014-internal-knowledge-base.md) 与 [CONTEXT.md](CONTEXT.md)。

## 快速开始

**环境要求：** Git、Node.js/npm、Docker 兼容运行环境（OrbStack、Docker Desktop）与 Docker Compose。镜像在本地构建，未发布安装包或公共镜像。

```bash
git clone https://github.com/Dikui/Reliquary.git
cd Reliquary
cp .env.example .env   # 配置数据库密码、SYSTEM_AES_KEY、NEO4J_PASSWORD 与模型服务地址
./scripts/build-frontend-dist.sh
./reliquary-admin check-deployment
docker compose up -d --wait postgres redis neo4j
./reliquary-admin check-deployment
docker compose up -d --build
```

启动后访问：

- 网页：<http://localhost>
- API：<http://localhost:8080>
- MCP：`http://localhost:8080/mcp`

没有登录。端口只绑定 `127.0.0.1`，在本机打开网页即可使用；外部工具通过 API Key 访问（[ADR 0015](docs/adr/0015-no-login-local-only.md)）。

宿主机目录默认为 `~/Reliquary/vault`、`~/Reliquary/files` 与 `~/Backups/Reliquary`（`.env` 中的 `RELIQUARY_VAULT_DIR`、`RELIQUARY_FILES_DIR`、`BACKUP_DIR`），不要放在“桌面”或“文稿”下。`check-deployment` 会创建缺失目录并拒绝不安全的路径。

> [!TIP]
> 长期使用请按[本地部署指南](docs/local-deployment.md)安装，将运行配置和数据放在源码目录之外。

## 使用

### 接入外部工具

1. 打开网页，在 **设置 → API 集成** 为每个工具创建独立的 API Key。
2. 用 `Authorization: Bearer <KEY>` 调用 HTTP API：

   ```bash
   curl --fail http://127.0.0.1:8080/api/v1/libraries -H "Authorization: Bearer $KEY"
   ```

3. 或将 MCP 客户端指向 `http://127.0.0.1:8080/mcp`，使用同一个 Key。Claude Code、Codex 的配置写法及 agent 技能包见 [skills/reliquary/SKILL.md](skills/reliquary/SKILL.md#连接本机-mcp)。

HTTP 与 MCP 中资料 ID 统一为 `entry_id`（列表为 `entry_ids`），全文读取使用 `get_entry`。从[外部接入总览](docs/external-integration.md)开始，再看[收录](docs/ingest-api.md)、[检索](docs/search-api.md)与[关系](docs/relations-api.md)接口。

### 用 Obsidian 阅读

在 Obsidian 打开 `vault/` 这一层。将 `attachments/` 与 `.reliquary/` 排除在搜索之外，不要修改资料表头和 `.reliquary/`——表头由数据库生成，刷新与重建会覆盖手改。详见[维护数据](docs/maintenance-data.md#按库文件与统一表头)。

## 备份与升级

```bash
./reliquary-admin backup /path/to/backups
./reliquary-admin upgrade
```

`upgrade` 会暂停收录，迁移前完整备份旧代码、旧镜像、数据库、文件库与资源。备份不含可用凭据，恢复后需重新填写模型凭据；`restore` 要求目标目录为空或不存在。详见[个人使用指南](docs/personal-use.md)与[迁移规范](docs/database-migrations.md)。

## 开发

```bash
make dev-start      # 启动 PostgreSQL、Redis、Neo4j
make dev-app        # 后端，另开终端
make dev-frontend   # 前端，另开终端
```

检查：

```bash
go build ./... && go vet ./...
GOFLAGS=-p=1 go test -count=1 -timeout=40m ./...
cd frontend && npm run type-check && npm run build && npm test
```

集成测试需要真实的 PostgreSQL、Redis 与 Neo4j，见[开发指南](docs/development.md#系列联合验证)。Issues 与 specs 以本地 Markdown 存放在 `.scratch/<feature-slug>/`（[约定](docs/agents/issue-tracker.md)）。

## 文档

全部指南见 [docs/index.md](docs/index.md)，架构决策见 [docs/adr/](docs/adr/)。

## 许可证

[MIT](LICENSE)。

Reliquary 源自 [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0)（提交 `1edcd54b`），保留上游署名与第三方许可，详见 [UPSTREAM.md](UPSTREAM.md)。
