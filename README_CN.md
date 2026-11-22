<div align="center">

# Reliquary

**个人 AI 工作平台的本机单用户资料库。**

外部 AI 工具通过 HTTP 或 MCP 送入可读 Markdown，Reliquary 负责存储、索引、关联与检索。

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

Reliquary 是个人 AI 工作平台的内部资料库。它不抓取任何内容：Grok Bot、外部 Agent、采集程序负责取得原文、转换为 Markdown 并上传图片后提交；Reliquary 负责技术校验、保存、索引，在后台抽取实体与关系，并向这些工具提供普通检索和图谱检索。

个人空间允许零库。用户按来源与可信度自行建库，所有库统一使用通用模板。外部工具先读取实际库清单，再选择目标库。

网页用于管理资料和阅读已保存正文。需要离线阅读时，可显式导出文件库，用 [Obsidian](https://obsidian.md) 打开。

## 功能

- **只收录** —— 可读 Markdown 和可选图片经带认证的 HTTP 收录接口或 MCP 送入。空内容、纯链接、仅图片等技术校验失败会报错；新资料不做模型准入审核，历史待审资料保留显式人工操作。
- **联合处理** —— 后台生成摘要、内容类型、实体、主题和有原文依据的关系；长文分段处理并保留检查点。
- **全文保存** —— Markdown 正文保存在 PostgreSQL，原始图片保存在磁盘。显式导出时才按库生成 Markdown 文件和数据库表头。
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

需要 macOS 或 Linux、Python 3.9+、Git 和已启动的 Docker Compose v2。Go、Node 和全部锁定依赖在容器内下载构建，宿主机无需安装。

```bash
git clone https://github.com/Dikui/Reliquary.git
cd Reliquary
python3 scripts/install-local.py --install --root "$HOME/Reliquary"
```

该命令构建镜像、创建独立运行目录并启动资料库本体，随后显示数据外流说明并询问是否配置本地 Pi，默认选择是。本地 Pi 是推荐的 Agent 入口，可以跳过。跳过或非交互运行时会打印稍后配置的命令：

```bash
python3 "$HOME/Reliquary/scripts/agent-local.py" pi-configure
```

按提示选择自己的模型 API 或已授权的 ChatGPT 模型、工作文件夹（默认 `~/Reliquary/workspace`）和可选代理。Pi 在 Docker 内运行，宿主机无需安装 Node。工作文件夹以只读方式挂载到 `/workspace`；工具只开放内置 read、Reliquary MCP 和项目的两个上传工具。Excel 导入必须走导入方案，并经审核确认。配置时会实际检查 `list_libraries` 调用，随后给出启动命令：

```bash
"$HOME/Reliquary/scripts/pi"
"$HOME/Reliquary/scripts/pi" -c  # 继续上次会话
```

> [!IMPORTANT]
> 启用本地 Pi 前，须同意将每轮完整上下文（问题、工具读到的资料片段、从工作文件夹读取的内容、历史回复）发送给所选模型服务商；ChatGPT 订阅经 Reliquary 后端转发给 OpenAI。资料库正文、文件和数据库不会整体同步出去。本地 Pi 会话暂未纳入 `reliquary-admin` 备份。见 [ADR 0029](docs/adr/0029-recommend-local-pi.md) 和 [Agent 辅助安装指南](docs/agent-installation.md)。

### 可选的群聊服务

在运行目录放置所选平台的接入模块、依赖锁和预期入口名单。执行 `python3 "$HOME/Reliquary/scripts/agent-local.py" configure` 选择资料库和模型并启动共享 Pi 服务。各入口分别保存会话。[接入与部署指南](agents/chat/README.md)

群消息经所选平台传输。每轮提问、工具读取的资料片段及相关对话历史会发送给所选模型服务商。

默认网页为 <http://127.0.0.1>，API 为 <http://127.0.0.1:8080>，MCP 为 `http://127.0.0.1:8080/mcp`。端口冲突时增加 `--app-port 18080 --frontend-port 18081`。同机第二套安装还需独立 `--project-name` 和 `.env` 中的 `REDIS_PORT`。

资料库数据存于本机。网页采用本机免登录访问，外部工具使用 API Key。完整安装、存储、授权、更新与恢复步骤见[本地部署指南](docs/local-deployment.md)，群聊使用方法见[Agent 指南](agents/chat/README.md)。

## 使用

### 接入外部工具

1. 打开网页，在 **设置 → API 集成** 为每个工具创建独立的 API Key。
2. 用 `Authorization: Bearer <KEY>` 调用 HTTP API：

   ```bash
   curl --fail http://127.0.0.1:8080/api/v1/libraries -H "Authorization: Bearer $KEY"
   ```

3. 或将 MCP 客户端指向 `http://127.0.0.1:8080/mcp`，使用同一个 Key。推荐本地 Pi，通用客户端仍可接入。Claude Code、Codex 的配置写法及 agent 技能包见 [skills/reliquary/SKILL.md](skills/reliquary/SKILL.md#连接本机-mcp)。

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
