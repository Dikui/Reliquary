# Reliquary

The three core services are PostgreSQL, Redis, and Reliquary. PostgreSQL owns business data and pending operations; Redis schedules tasks. See [ADR 0016](docs/adr/0016-store-and-track-event-layer.md).

[简体中文](README_CN.md)

Reliquary is an internal knowledge repository for a personal AI workspace. It stores and retrieves entries across five system libraries and libraries created from templates. External AI tools use HTTP and MCP for retrieval, analysis, and report writing. Start with the [external integration guide](docs/external-integration.md) for Grok Bot and other callers.

The source originated from [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0), commit `1edcd54b43606d9079bb36650efe3f68707a79ea`. See [UPSTREAM.md](UPSTREAM.md) for provenance and retained licensing.

## Features

| Capability | Current behavior |
| --- | --- |
| Intake | External tools send complete content through the authenticated HTTP intake API or MCP; Reliquary does not fetch source content |
| Intake review | Submitted content passes intake review; rejected content and missing bodies remain pending until you approve, supplement, or discard them |
| Entry processing | Produces summaries; long content uses durable segment checkpoints |
| Storage | Retains full Markdown text and supplied images |
| File library | Every library has its own English directory: `official/`, `media/`, `references/`, `personal/`, `ai-output/`, or a custom directory. All use the same database-owned header and full Markdown body; images live in `attachments/<entry-id>/` |
| Search | Hybrid full-text and vector retrieval; filter by library, publication dates and stars, with each entry shown separately |
| Personal decisions | Star entries, merge entities, and inspect the operation log |
| External AI | MCP supports search, entry reading, collection, pending review, stars, entity lookup, and collecting AI output |
| Evaluation and accounting | Question sets, five retrieval metrics, run comparison, and model call price snapshots |
| Administration | Library, models, file library status, operation log, API integration, and runtime queues |

Article bodies become final when all missing material is supplied or you accept the missing material. PostgreSQL stores indexes, a maintained entity directory, review state, personal decisions, and operation logs. The database generates all library headers in one direction; refresh and rebuild overwrite manual edits in Obsidian. File names use a title slug and the first eight ID characters, with no monthly subdirectories. See [header fields and replica registration](docs/maintenance-data.md#按库文件与统一表头).

The Web UI provides entry management and simple reading. Open the file library in Obsidian for regular reading, and use external AI tools for analysis and report writing. Content enters only through external tools using HTTP intake or MCP, as required Markdown text with supplied images. Images are uploaded by the caller. Missing images remain visible as missing material. RSS subscriptions, URL imports, browser uploads, and manual entry are removed. Existing entries and their RSS or Web source labels remain readable. See [ADR 0012](docs/adr/0012-ingest-only-no-collection.md), [CONTEXT.md](CONTEXT.md), and the [intake](docs/ingest-api.md), [search](docs/search-api.md), [evaluation](docs/search-evaluation-api.md), and [model calls](docs/model-calls.md) references.

## Run from source

For regular use, follow the [source-independent deployment guide](docs/local-deployment.md) to keep runtime configuration and data outside the checkout. The commands below run directly from the source directory.

Requirements: Git, Node.js/npm, a runtime compatible with Docker (such as OrbStack or Docker Desktop), and Docker Compose. Application images are built locally; this repository does not publish installation packages or public application images.

```bash
git clone https://github.com/Dikui/Reliquary.git
cd Reliquary
cp .env.example .env
# Configure database credentials, SYSTEM_AES_KEY, and model endpoints.
./scripts/build-frontend-dist.sh
./reliquary-admin check-deployment
docker compose up -d --wait postgres redis
./reliquary-admin check-deployment
docker compose up -d --build
```

The frontend build helper installs dependencies with `npm ci` during a normal installation. Compose binds host directories: `RELIQUARY_VAULT_DIR` defaults to `${HOME}/Reliquary/vault`, `RELIQUARY_FILES_DIR` to `${HOME}/Reliquary/files`, and `BACKUP_DIR` to `${HOME}/Backups/Reliquary`. Change them in `.env`. `check-deployment` creates missing directories and rejects unwritable paths, paths inside the source checkout, and data or backup directories that contain one another. The PostgreSQL volume is unchanged. See [the Mac mini deployment guide](docs/personal-use.md#部署在-mac-mini).

Keep the file library, uploaded resources, and backups outside the Desktop and Documents folders.

Open the `vault/` directory itself in Obsidian. Exclude `attachments/` and `.reliquary/` from Obsidian search, and leave generated entry headers and `.reliquary/` unchanged. Reading settings in `.obsidian/` are backed up and ignored during verification and indexing.

By default the Web UI is at `http://localhost`, and the backend API is at `http://localhost:8080`. The first successful registration closes further registration. The single user and valid API keys have full access to the personal space. Deploy on your private network.

Apple Silicon model serving can run natively on macOS with Ollama and Metal. Model configuration supports Ollama, OpenAI-compatible endpoints, and native Anthropic generation; embeddings and reranking use their corresponding supported interfaces. See [the personal use guide](docs/personal-use.md) for setup and external integration.

## Backup and migration

Use `./reliquary-admin upgrade` for upgrades. It pauses intake and creates a complete backup of the old code, images, database, file library, and resources. Before migration `000005` drops collection tables, it exports source configuration without credentials and sync logs to `.reliquary/exports/` inside the file library. Export failure stops the upgrade and leaves intake paused. Old backups still restore through the same export and migration steps.

```bash
./reliquary-admin backup /path/to/backups
./reliquary-admin upgrade
# Before running development migrations directly:
./reliquary-admin export-sources
```

Backups retain host files and Obsidian reading settings while excluding operational credentials. `unpack` and `restore` require empty or nonexistent host file library and resource directories. Configure model credentials again after restoring. See [the personal use guide](docs/personal-use.md), [migration rules](docs/database-migrations.md), and [maintenance data ownership](docs/maintenance-data.md).

## Development and validation

```bash
make dev-start
make dev-app       # Separate terminal
make dev-frontend  # Separate terminal
```

See [the development guide](./docs/development.md). Issues and specs are local Markdown files under `.scratch/<feature-slug>/`, following [the issue tracker conventions](docs/agents/issue-tracker.md).

```bash
go build ./...
go vet ./...
GOFLAGS=-p=1 go test -count=1 -timeout=40m ./...
cd frontend
npm run type-check
npm run build
npm test
```

Integration tests require PostgreSQL and Redis endpoints documented in the [development guide](docs/development.md#系列联合验证). Format changed Go files with `gofmt` and report baseline or environment failures with the exact command.

## License

[MIT](LICENSE). Upstream attribution and third-party licenses remain in the repository.

Entry IDs use `entry_id` and lists use `entry_ids` across HTTP and MCP. Read content with `get_entry`. Submit generated material through `ingest` and cite sources in its Markdown body; existing report references remain readable. Retired field and tool aliases are not accepted.
