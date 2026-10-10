# Reliquary

基于 [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0) 的首版源码底座。
本仓库的项目名称为 Reliquary，程序内部标识遵循各语言的大小写规范，保留上游功能、许可和第三方服务。

- 仓库：http://192.168.1.146:3000/dikui/Reliquary
- 上游提交：`1edcd54b43606d9079bb36650efe3f68707a79ea`
- 来源、命名范围与保留项：[UPSTREAM.md](UPSTREAM.md)

## 从源码启动

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env
# 按 .env 中的说明配置模型服务、数据库密码等参数。
./scripts/build_frontend_dist.sh
docker compose up -d --build
```

需要 Git、Node.js/npm 和 Docker Compose。镜像使用本地 `reliquary-*` 名称并从本仓库构建；
本首版未发布预编译程序、Homebrew 安装包或公共镜像。下方及其他文档中的上游发布链接仅用于参考，
使用本项目请采用上述源码构建方式。

Go 主模块为 `reliquary`；
环境变量前缀为 `RELIQUARY_`。此版本面向全新安装，不自动迁移旧 WeKnora 数据目录、数据库或浏览器设置。2026-10-07 已将 PostgreSQL `000000` 至 `000098` 迁移压缩为新的 `000000_init` 基线，旧 Reliquary 数据库也必须重建，不能原地升级；操作说明见 [数据库迁移排障](docs/migration-troubleshooting.md#single-baseline-and-database-rebuild)。

本版本供个人单用户使用，首次注册后关闭注册。登录用户和有效 API Key 拥有全部权限，密码恢复使用本地命令，详见[个人使用说明](docs/personal-use.md)。

知识库新增对象与事实、证据历史、多源事件，以及定时研究和报告工作流。架构、配置、数据升级和使用方法见 [知识库融合说明](docs/知识库融合说明.md)。

## 上游功能与使用说明

## 📌 Overview

[**Reliquary**](https://weknora.weixin.qq.com) is an open-source, LLM-powered knowledge framework built for enterprise-grade document understanding, semantic retrieval, and autonomous reasoning.

Reliquary provides **RAG-based Quick Q&A**, a **ReAct Agent** with knowledge retrieval, MCP tools and in-process data analysis, and scheduled knowledge reports. Folder import, chunk editing with revision history, RSS feeds, site adapters, full-access API keys and model integrations support personal knowledge management. Data remains in PostgreSQL and local file storage.

The framework supports auto-syncing RSS feeds and site adapters, handles 10+ document formats including PDF, Word, images, Excel and EPUB. It is compatible with major LLM providers including OpenAI, DeepSeek, Qwen (Alibaba Cloud), Zhipu, Hunyuan, Gemini, MiniMax, NVIDIA, LiteLLM, and Ollama. Office files can be parsed in-process with **anydoc**. Its fully modular design supports configurable LLMs, PostgreSQL retrieval, and local file storage, with support for local and private cloud deployment ensuring complete data sovereignty. Model usage is recorded in the `model_usage` table.


## 📱 Interface Showcase

<table>
      <tr>
    <td colspan="2" align="center"><b>💬 Intelligent Q&A Conversation</b><br/><img src="./docs/images/qa.png" alt="Intelligent Q&A Conversation" width="100%"></td>
  </tr>
    <tr>
    <td width="50%" align="center"><b>✂️ Chunk Editing & Revision History</b><br/><img src="./docs/images/kb-chunk-edit.png" alt="Chunk Editing and Revision History" width="100%"></td>
  </tr>
  <tr>
    <td width="50%" align="center"><b>📁 Folder Tree & Batch Operations</b><br/><img src="./docs/images/kb-document-list.png" alt="Knowledge Base Folder Tree and Batch Operations" width="100%"></td>
  </tr>
</table>

## 🏗️ Architecture

Upstream v0.8.0 architecture reference. See the [personal use guide](docs/personal-use.md) for the current supported features.

![reliquary-architecture.png](./docs/images/architecture.png)

Fully modular pipeline from document parsing, vectorization, and retrieval to LLM inference — every component is swappable and extensible. Supports local / private cloud deployment with full data sovereignty and a zero-barrier Web UI for quick onboarding.

## 🧩 Feature Overview

**Intelligent Conversation**

| Capability | Details |
|------------|---------|
| Intelligent Reasoning | ReACT progressive multi-step reasoning, autonomously orchestrating knowledge retrieval, MCP tools and data analysis |
| Quick Q&A | RAG-based Q&A over knowledge bases for fast and accurate answers |
| Tool Calling | Built-in tools, MCP tools (incl. OAuth2 remote services, mid-conversation OAuth); `@MCP` mentions to scope the agent runtime per turn |
| Conversation Strategy | Online Prompt editing, retrieval threshold tuning, multi-turn context awareness, per-agent citation output toggle |
| Suggested Questions | Auto-generated question suggestions and after-answer follow-ups based on knowledge base content |
| Temporary Attachments | Session-scoped image / document uploads with async parsing for one-off Q&A, with a combined image + attachment limit |
| Citations & RAG Progress | Inline citation popovers and a references drawer, shared markdown rendering, and stage-by-stage RAG pipeline progress in chat |
| Session Management | Browse sidebar sessions with inline session-title rename |

**Knowledge Management**

| Capability | Details |
|------------|---------|
| Knowledge Base Types | Document with folder import, URL import, multi-tag management, and online entry |
| Folder Tree | Folder uploads keep their original directory structure, with a sidebar tree for browsing, folder rename, and re-filing documents into another folder |
| Chunk Editing & Revisions | Edit retrieval chunks directly in the UI with per-version snapshots, diff and one-click rollback, and automatic reindexing after an edit; generated questions can be added, edited, deleted and regenerated; custom document metadata supported |
| Per-Upload Process Config | Override parser, chunking, multimodal (VLM / ASR), and question generation per upload batch via upload-confirm dialog or `process_config` API; reparse with new settings |
| Batch Reparse | Re-queue parsing for multiple documents at once with optional per-batch `process_config` |
| Data Source Import | RSS, Atom, JSON Feed and site adapters with incremental and full sync |
| Document Formats | PDF / Word / Txt / Markdown / EPUB / Images / CSV / Excel / PPT / JSON |
| Auto-Tagging | After parse, pick matching tags from the knowledge base's existing set without creating tags or overwriting manual ones |
| Retrieval Strategies | BM25 sparse / Dense retrieval / parent-child chunking / HNSW-accelerated pgvector (1024-dim) / multi-dimensional indexing |
| Batch Selection & Tagging | Marquee drag-select multiple documents in the KB list for batch reparse and batch tagging (common tags pre-selected) |

**Integrations & Extensions**

| Capability | Details |
|------------|---------|
| LLMs | OpenAI / Azure OpenAI / Anthropic (Claude) / DeepSeek / Qwen (Alibaba Cloud) / Zhipu / Hunyuan / Doubao (Volcengine) / Gemini / MiniMax / NVIDIA / Novita AI / SiliconFlow / OpenRouter / Requesty / LiteLLM / Ollama |
| Embeddings | Ollama / BGE / GTE / Zhipu / OpenAI-compatible APIs |
| Vector DB | PostgreSQL (ParadeDB: pgvector + pg_search) |
| Object Storage | Local filesystem |
| API Integration | Full-access API keys with an API integration playground; `resource_urls=public` returns directly loadable file/image URLs |

**Platform**

| Capability | Details |
|------------|---------|
| Deployment | Docker Compose with private and offline support |
| UI | Web UI / RESTful API |
| Access Control | One personal user with complete access, automatic registration closure, full-access API keys and local password recovery |
| Security | AES-256-GCM at-rest encryption for API keys and MCP / data-source credentials with graceful key rotation; Redis TLS; SSRF-safe HTTP client (data sources, URL import, redirect chains); secret redaction in responses |
| Observability | Model usage in `model_usage`, document processing timelines, runtime task queues and worker governance |
| Task Management | MQ async tasks with per-stage worker-pool governance (core / post-process / enrichment / maintenance + elastic shared pool) and per-model background concurrency governors; automatic database migration on version upgrade |
| Model Management | Centralized config, declarative built-in models via YAML, per-knowledge-base model selection, per-model thinking-mode and embedding-dimension overrides, interactive model test debugger, WeKnora Cloud hosted models and parsing |

## 🧩 Chrome Extension

[**Reliquary Chrome Extension**](https://chromewebstore.google.com/detail/jpemjbopikggjlmikmclgbmkhhopjdgd) lets you capture web content directly into your Reliquary knowledge base. Select text, images, or entire pages in the browser and save them as knowledge entries with one click — no copy-paste or file upload needed.


## 🚀 Getting Started

### 🛠 Prerequisites

- [Docker](https://www.docker.com/) & [Docker Compose](https://docs.docker.com/compose/)
- [Git](https://git-scm.com/)

### 📦 Installation & Launch

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env   # Edit .env as needed, see comments in the file
./scripts/build_frontend_dist.sh
docker compose up -d --build
```

Once started, visit **http://localhost** to get started.

> To use a local Ollama model, run `ollama serve > /dev/null 2>&1 &` first.

### 🌐 Service URLs

| Service | URL |
|---------|-----|
| Web UI | `http://localhost` |
| Backend API | `http://localhost:8080` |

## 📘 API Reference

Start with the [personal use guide](./docs/personal-use.md) and [knowledge workflow guide](./docs/知识库融合说明.md).

Troubleshooting FAQ: [Troubleshooting FAQ](./docs/QA.md)

## 🧭 Developer Guide

### ⚡ Fast Development Mode (Recommended)

If you need to frequently modify code, **you don't need to rebuild Docker images every time**! Use fast development mode:

```bash
# Start infrastructure
make dev-start

# Start backend (new terminal)
make dev-app

# Start frontend (new terminal)
make dev-frontend
```

**Development Advantages:**
- ✅ Frontend modifications auto hot-reload (no restart needed)
- ✅ Backend modifications quick restart (5-10 seconds, supports Air hot-reload)
- ✅ No need to rebuild Docker images
- ✅ Support IDE breakpoint debugging

**Detailed Documentation:** [Development Environment Quick Start](./docs/开发指南.md)


## 🤝 Contributing

Welcome to submit [Issues](https://github.com/Tencent/WeKnora/issues) or Pull Requests.

**Process:** Fork → Create branch → Commit changes → Open PR

**Standards:** Format code with `gofmt`, follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:` / `fix:` / `docs:` / `test:` / `refactor:`)

### Validation

For a focused PR, validate the changed scope first:

```bash
git fetch origin main
git diff --check origin/main...HEAD
golangci-lint run --new-from-rev=origin/main ./...
go test ./path/to/changed/package -count=1
```

Run `gofmt` on changed Go files before committing. For frontend changes, run the relevant tests from `frontend/` and use `npm run type-check` when the change affects TypeScript or Vue components.

The full maintainer gate remains:

```bash
make fmt
make lint
make test
```

`make fmt` formats the entire Go repository, so run it only with a clean worktree and review the resulting diff. Some full-suite tests require local infrastructure or service configuration. If a full check fails for an unrelated baseline or environment reason, include the exact command and failure in the PR while still providing passing targeted tests for your change.

## 🔒 Security Notice

**Important:** Starting from v0.1.3, Reliquary includes login authentication functionality to enhance system security. For production deployments, we strongly recommend:

- Deploy Reliquary services in internal/private network environments rather than public internet
- Avoid exposing the service directly to public networks to prevent potential information leakage
- Configure proper firewall rules and access controls for your deployment environment
- Regularly update to the latest version for security patches and improvements

## 👥 Contributors

Thanks to these excellent contributors:

[![Contributors](https://contrib.rocks/image?repo=Tencent/WeKnora)](https://github.com/Tencent/WeKnora/graphs/contributors)

## 📄 License

This project is licensed under the [MIT License](./LICENSE).
You are free to use, modify, and distribute the code with proper attribution.
