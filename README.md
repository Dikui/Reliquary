# Reliquary

Background jobs and application data persist in PostgreSQL, with transactional enqueue and crash recovery. Deployments require no separate queue service. See [ADR 0010](docs/adr/0010-postgres-task-queue.md).

[简体中文](README_CN.md)

Reliquary is a personal information component for collecting, storing, deduplicating, and retrieving articles. One personal space contains one knowledge base. External AI tools perform analysis and write reports through the intake API and MCP.

The source originated from [Tencent WeKnora v0.8.0](https://github.com/Tencent/WeKnora/releases/tag/v0.8.0), commit `1edcd54b43606d9079bb36650efe3f68707a79ea`. See [UPSTREAM.md](UPSTREAM.md) for provenance and retained licensing.

## Features

| Capability | Current behavior |
| --- | --- |
| Collection | Global RSS subscriptions, file uploads, URL import, and an authenticated intake API for external collectors |
| Intake review | Subscription content passes intake review; rejected content remains in the pending area until you approve or discard it |
| Article processing | One processing call produces a content type, a common summary, entities, and user-defined topics; processing models with vision can read article images in the same call |
| Storage modes | Events, opinions, and research retain their full text, images, and original files; tutorials, cases, and references retain a summary and original link |
| File library | Approved articles become Markdown files with generated headers; `mine/` contains your documents and `reports/` contains saved analysis |
| Search | Hybrid full-text and vector retrieval, merged article results, filters for content type, entities, topics, publication dates, stars, and source directory; similar reporting can be collapsed |
| Personal decisions | Star articles, correct content types and topics, merge entities, and inspect the operation log; new topics can classify historical article summaries |
| External AI | MCP supports search, article reading, collection, pending review, stars, entity/topic lookup, and writing saved analysis |
| Administration | Knowledge base, subscriptions, models, file library status, operation log, API integration, and runtime queues |

Article bodies remain unchanged after their first write. PostgreSQL stores indexes, entity profiles, topics, review state, personal decisions, and operation logs. The database generates article headers in one direction; manual header edits in Obsidian may be overwritten. Your documents are indexed without being edited by the application.

The Web UI provides article management and simple reading. Open the file library in Obsidian for regular reading, and use external AI tools for analysis and report writing. See [CONTEXT.md](CONTEXT.md) and [ADR 0009](docs/adr/0009-kb-collect-store-retrieve.md) for terminology and design.

## Run from source

Requirements: Git, Node.js/npm, Docker, and Docker Compose. Application images are built locally; this repository does not publish installation packages or public application images.

```bash
git clone http://192.168.1.146:3000/dikui/Reliquary.git
cd Reliquary
cp .env.example .env
# Configure database credentials, JWT_SECRET, SYSTEM_AES_KEY, and model endpoints.
./scripts/build_frontend_dist.sh
docker compose up -d --build
```

The frontend build helper installs dependencies with `npm ci` during a normal installation. Configure the readable file library and its permissions before starting the application, as described in [the Mac mini deployment guide](docs/personal-use.md#部署在-mac-mini).

By default the Web UI is at `http://localhost`, and the backend API is at `http://localhost:8080`. The first successful registration closes further registration. The single user and valid API keys have full access to the personal space. Deploy on your private network.

Apple Silicon model serving can run natively on macOS with Ollama and Metal. Model configuration supports Ollama, OpenAI-compatible endpoints, and native Anthropic generation; embeddings and reranking use their corresponding supported interfaces. See [the personal use guide](docs/personal-use.md) for setup and external integration.

## Backup and migration

Back up PostgreSQL, the file library, and uploaded resources together while application writes and file edits are paused. Copy deployment configuration and encryption keys separately. The [migration guide](docs/personal-use.md#迁移到另一台-mac-mini) describes restoring onto another Mac mini, checking articles and search, and rebuilding from the file library when a database backup is unavailable.

```bash
docker compose stop app frontend
./scripts/backup.sh /path/to/new-backup
# On the destination, start PostgreSQL and create the stopped app container first.
./scripts/restore.sh /path/to/new-backup
```

Both scripts accept a backup directory and honor `COMPOSE_FILE` and `COMPOSE_PROJECT_NAME`. Run `--help` for host directory overrides and empty destination requirements.

This release requires a new installation when replacing older WeKnora or Reliquary schemas. The migration described above transfers the current schema between machines; it does not upgrade older schemas. See [database baseline rebuild guidance](docs/migration-troubleshooting.md#single-baseline-and-database-rebuild).

## Development and validation

```bash
make dev-start
make dev-app       # Separate terminal
make dev-frontend  # Separate terminal
```

See [the development guide](docs/开发指南.md). Issues and specs are local Markdown files under `.scratch/<feature-slug>/`, following [the issue tracker conventions](docs/agents/issue-tracker.md).

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
