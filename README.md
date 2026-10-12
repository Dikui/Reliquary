# Reliquary

Background jobs and application data persist in PostgreSQL, with transactional enqueue and crash recovery. Deployments require no separate queue service. See [ADR 0010](docs/adr/0010-postgres-task-queue.md).

[简体中文](README_CN.md)

Reliquary is a personal information component for collecting, storing, deduplicating, and retrieving articles. One personal space contains one knowledge base. External AI tools perform analysis and write reports through the intake API and MCP.

The source originated from [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0), commit `1edcd54b43606d9079bb36650efe3f68707a79ea`. See [UPSTREAM.md](UPSTREAM.md) for provenance and retained licensing.

## Features

| Capability | Current behavior |
| --- | --- |
| Intake | External tools send complete content through the authenticated HTTP intake API or MCP; Reliquary does not fetch source content |
| Intake review | Submitted content passes intake review; rejected content and missing bodies remain pending until you approve, supplement, or discard them |
| Article processing | One processing call produces a content type, a common summary, entities, and user-defined topics; processing models with vision can read article images in the same call |
| Storage modes | Events, opinions, and research retain their full text, images, and original files; tutorials, cases, and references retain a summary and original link |
| File library | Approved articles become Markdown files with generated headers; `mine/` contains your documents and `reports/` contains saved analysis |
| Search | Hybrid full-text and vector retrieval, merged article results, filters for content type, entities, topics, publication dates, stars, and source directory; similar reporting can be collapsed |
| Personal decisions | Star articles, correct content types and topics, merge entities, and inspect the operation log; new topics can classify historical article summaries |
| External AI | MCP supports search, article reading, collection, pending review, stars, entity/topic lookup, and writing saved analysis |
| Administration | Knowledge base, models, file library status, operation log, API integration, and runtime queues |

Article bodies become final when all missing material is supplied or you accept the missing material. PostgreSQL stores indexes, entity profiles, topics, review state, personal decisions, and operation logs. The database generates article headers in one direction; manual header edits in Obsidian may be overwritten. Your documents are indexed without being edited by the application.

The Web UI provides article management and simple reading. Open the file library in Obsidian for regular reading, and use external AI tools for analysis and report writing. Content enters only through external tools using HTTP intake or MCP, including original files and images. When reading articles, the browser still loads images directly from the original website. The backend does not download or proxy them, and missing material records remain unchanged. RSS subscriptions, URL imports, browser uploads, and manual entry are removed. Existing articles and their RSS or Web source labels remain readable. See [ADR 0012](docs/adr/0012-ingest-only-no-collection.md), [CONTEXT.md](CONTEXT.md), and the intake API reference planned for task 4 at `docs/ingest-api.md`.

## Run from source

Requirements: Git, Node.js/npm, a runtime compatible with Docker (such as OrbStack or Docker Desktop), and Docker Compose. Application images are built locally; this repository does not publish installation packages or public application images.

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env
# Configure database credentials, JWT_SECRET, SYSTEM_AES_KEY, and model endpoints.
./scripts/build-frontend-dist.sh
./reliquary-admin check-deployment
docker compose up -d --wait postgres
./reliquary-admin check-deployment
docker compose up -d --build
```

The frontend build helper installs dependencies with `npm ci` during a normal installation. Compose binds host directories: `RELIQUARY_VAULT_DIR` defaults to `${HOME}/Reliquary/vault`, `RELIQUARY_FILES_DIR` to `${HOME}/Reliquary/files`, and `BACKUP_DIR` to `${HOME}/Backups/Reliquary`. Change them in `.env`. `check-deployment` creates missing directories and rejects unwritable paths, paths inside the source checkout, and data or backup directories that contain one another. The PostgreSQL volume is unchanged. See [the Mac mini deployment guide](docs/personal-use.md#部署在-mac-mini).

Keep the file library, uploaded resources, and backups outside the Desktop and Documents folders.

Open the `vault/` directory itself in Obsidian. Exclude `attachments/` and `.reliquary/` from Obsidian search, and leave generated article headers and `.reliquary/` unchanged. Reading settings in `.obsidian/` are backed up and ignored during verification and indexing.

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

Backups retain host files and Obsidian reading settings while excluding operational credentials. `unpack` and `restore` require empty or nonexistent host file library and resource directories. Configure model and parser credentials again after restoring. See [the personal use guide](docs/personal-use.md), [migration rules](docs/database-migrations.md), and [maintenance data ownership](docs/maintenance-data.md).

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
go test -count=1 ./...
cd frontend
npm run type-check
npm run build
npm test
```

Database integration tests require the PostgreSQL test DSN in `RELIQUARY_TEST_PG_DSN`. Format changed Go files with `gofmt` and report baseline or environment failures with the exact command.

## License

[MIT](LICENSE). Upstream attribution and third-party licenses remain in the repository.
