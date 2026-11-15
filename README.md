<div align="center">

# Reliquary

**A local, single-user knowledge repository for your personal AI workspace.**

External AI tools feed it readable Markdown over HTTP or MCP; Reliquary stores, indexes, links and retrieves it.

[![App](https://github.com/Dikui/Reliquary/actions/workflows/app.yml/badge.svg)](https://github.com/Dikui/Reliquary/actions/workflows/app.yml)
[![Frontend](https://github.com/Dikui/Reliquary/actions/workflows/frontend.yml/badge.svg)](https://github.com/Dikui/Reliquary/actions/workflows/frontend.yml)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

English | [简体中文](README_CN.md)

</div>

## Table of contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Usage](#usage)
- [Backup and upgrade](#backup-and-upgrade)
- [Development](#development)
- [Documentation](#documentation)
- [License](#license)

## Overview

Reliquary is the internal library behind a personal AI workspace. It does not crawl or fetch anything: external tools (Grok Bot, agents, collectors) convert sources to Markdown, upload images, and submit them. Reliquary validates and saves submissions, indexes them, extracts entities and relations in the background, and serves ordinary and graph search back to those tools.

A personal space can have zero or more libraries. Create libraries for your sources and trust levels; all use the standard template. Agents must list the actual libraries before selecting a destination.

The Web UI is for managing entries and reading saved bodies. Export the file library when you need offline Markdown reading in [Obsidian](https://obsidian.md).

## Features

- **Intake only** – readable Markdown and optional images arrive through the authenticated HTTP intake API or MCP. Technical validation rejects empty, link-only or image-only submissions. New submissions do not undergo model admission review; legacy pending items retain explicit manual actions.
- **Joint processing** – background processing produces summary, content type, entities, topics and supported relations. Long content is segmented with durable checkpoints.
- **Full-text storage** – Markdown bodies stay in PostgreSQL and original images stay on disk. Explicit export produces per-library Markdown with database-generated headers.
- **Hybrid search** – full-text plus vector retrieval, merged per entry, filterable by content type, entity, topic, publication date, star and library; similar reports can be collapsed.
- **Graph search** – 1–3 hop neighbourhoods or paths between entities, with source entries and body positions. No model calls.
- **Your judgement** – star entries, correct content types and topics, merge entities and relation types, and review the operation log.
- **MCP tools** – search, read entries, ingest, handle pending items, star, look up entities and topics, write AI output.
- **Evaluation** – question sets, five retrieval metrics, run comparison and model-call price snapshots.
- **Models** – Ollama (native Metal on Apple Silicon), OpenAI-compatible endpoints and native Anthropic for generation; embeddings and reranking through their supported interfaces.

## Architecture

| Service | Role |
| --- | --- |
| PostgreSQL | Source of truth: business data, indexes, review state, pending operations |
| Redis | Task scheduling |
| Neo4j | Rebuildable graph projection |
| Reliquary | Go backend (HTTP + MCP) and Vue 3 frontend |

See [ADR 0014](docs/adr/0014-internal-knowledge-base.md) and [CONTEXT.md](CONTEXT.md) for the domain model.

## Quick start

Use macOS or Linux with Python 3.9+, Git and a running Docker engine with Compose v2. The first build downloads Go, Node, Pi, the Feishu SDK and locked dependencies inside containers.

```bash
git clone https://github.com/Dikui/Reliquary.git
cd Reliquary
python3 scripts/install-local.py --install --root "$HOME/Reliquary"
python3 "$HOME/Reliquary/scripts/agent-local.py" configure
```

The installer builds images, creates a separate runtime directory and starts the base services. The configuration command selects libraries, your API provider or ChatGPT subscription, and your Feishu app and group. It checks a model tool call and starts the bot. Authorize ChatGPT in the Web UI when connecting an account for the first time. Existing connected models can be reused. Complete Feishu app permissions and account authorization yourself.

The default Web UI is <http://127.0.0.1>, API is <http://127.0.0.1:8080>, and MCP is `http://127.0.0.1:8080/mcp`. For occupied ports, add `--app-port 18080 --frontend-port 18081`. A second installation on the same host also needs a unique `--project-name` and `REDIS_PORT` in its environment file.

Data and conversations stay on the customer's machine. Local browser access uses the existing local-access policy. External tools authenticate with API keys. See the [local deployment guide](docs/local-deployment.md) for installation, storage, authorization and recovery, and the [agent guide](agents/feishu/README.md) for Feishu usage.

## Usage

### Connect an external tool

1. In the Web UI, go to **Settings → API Integration** and create one API key per tool.
2. Call the HTTP API with `Authorization: Bearer <KEY>`:

   ```bash
   curl --fail http://127.0.0.1:8080/api/v1/libraries -H "Authorization: Bearer $KEY"
   ```

3. Or point an MCP client at `http://127.0.0.1:8080/mcp` with the same key. Claude Code and Codex setup, plus the agent skill package, are in [skills/reliquary/SKILL.md](skills/reliquary/SKILL.md#连接本机-mcp).

Entry IDs are `entry_id` (lists: `entry_ids`) across HTTP and MCP; read bodies with `get_entry`. Start with the [external integration guide](docs/external-integration.md), then the [intake](docs/ingest-api.md), [search](docs/search-api.md) and [relations](docs/relations-api.md) references.

### Read in Obsidian

Open the `vault/` directory itself as a vault. Exclude `attachments/` and `.reliquary/` from search, and don't edit entry headers or `.reliquary/` – headers are generated by the database and refresh/rebuild overwrites manual edits. See [maintenance data](docs/maintenance-data.md#按库文件与统一表头).

## Backup and upgrade

```bash
./reliquary-admin backup /path/to/backups
./reliquary-admin upgrade
```

`upgrade` pauses intake and takes a full backup of the old code, images, database, file library and resources before migrating. Backups exclude credentials; re-enter model credentials after a restore. `restore` requires empty or missing target directories. Details: [personal use guide](docs/personal-use.md), [migration rules](docs/database-migrations.md).

## Development

```bash
make dev-start      # start PostgreSQL, Redis, Neo4j
make dev-app        # backend, in another terminal
make dev-frontend   # frontend, in another terminal
```

Checks:

```bash
go build ./... && go vet ./...
GOFLAGS=-p=1 go test -count=1 -timeout=40m ./...
cd frontend && npm run type-check && npm run build && npm test
```

Integration tests need real PostgreSQL, Redis and Neo4j; see the [development guide](docs/development.md#系列联合验证). Issues and specs live as local Markdown under `.scratch/<feature-slug>/` ([conventions](docs/agents/issue-tracker.md)).

## Documentation

All guides are indexed in [docs/index.md](docs/index.md); architecture decisions are in [docs/adr/](docs/adr/). Most docs are in Chinese.

## License

[MIT](LICENSE).

Reliquary is derived from [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0) (commit `1edcd54b`). Upstream attribution and third-party licenses are retained; see [UPSTREAM.md](UPSTREAM.md).
