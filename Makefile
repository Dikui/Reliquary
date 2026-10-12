.PHONY: help build run reset-password test clean docker-build-app docker-build-frontend docker-build-all docker-run migrate-up migrate-down docker-restart docker-stop start-all stop-all start-ollama stop-ollama build-images build-images-app build-images-frontend clean-images check-env list-containers pull-images show-platform dev-start dev-stop dev-restart dev-logs dev-status dev-app dev-frontend anydoc-lib build-anydoc

# Show help
help:
	@echo "Reliquary Makefile 帮助"
	@echo ""
	@echo "基础命令:"
	@echo "  build             构建应用"
	@echo "  run               运行应用"
	@echo "  reset-password    从标准输入重置唯一用户密码"
	@echo "  test              运行测试"
	@echo "  test-pg           运行含 PostgreSQL 集成用例的测试（设置 RELIQUARY_TEST_PG_DSN）"
	@echo "  anydoc-lib        构建 anydoc 静态库（需要 Rust 工具链）"
	@echo "  build-anydoc      构建带 anydoc 解析引擎的应用"
	@echo "  clean             清理构建文件"
	@echo ""
	@echo "Docker 命令:"
	@echo "  docker-build-app       构建应用 Docker 镜像 (reliquary-app)"
	@echo "  docker-build-frontend  构建前端镜像 (reliquary-ui)"
	@echo "  docker-build-all       构建所有 Docker 镜像"
	@echo "  docker-run            运行 Docker 容器"
	@echo "  docker-stop           停止 Docker 容器"
	@echo "  docker-restart        重启 Docker 容器"
	@echo ""
	@echo "服务管理:"
	@echo "  start-all         启动所有服务"
	@echo "  stop-all          停止所有服务"
	@echo "  start-ollama      仅启动 Ollama 服务"
	@echo ""
	@echo "镜像构建:"
	@echo "  build-images      从源码构建所有镜像"
	@echo "  build-images-app  从源码构建应用镜像"
	@echo "  从源码构建文档读取器镜像"
	@echo "  build-images-frontend  从源码构建前端镜像"
	@echo "  clean-images      清理本地镜像"
	@echo ""
	@echo "数据库:"
	@echo "  migrate-up        执行数据库迁移"
	@echo "  migrate-down      回滚数据库迁移"
	@echo ""
	@echo "开发工具:"
	@echo "  fmt               格式化代码"
	@echo "  lint              代码检查"
	@echo "  deps              安装依赖"
	@echo ""
	@echo "环境检查:"
	@echo "  check-env         检查环境配置"
	@echo "  list-containers   列出运行中的容器"
	@echo "  pull-images       拉取最新镜像"
	@echo "  show-platform     显示当前构建平台"
	@echo ""
	@echo "开发模式（推荐）:"
	@echo "  dev-start         启动开发环境基础设施（仅启动依赖服务）"
	@echo "  dev-stop          停止开发环境"
	@echo "  dev-restart       重启开发环境"
	@echo "  dev-logs          查看开发环境日志"
	@echo "  dev-status        查看开发环境状态"
	@echo "  dev-app           启动后端应用（本地运行，需先运行 dev-start）"
	@echo "                    已 make anydoc-lib 时自动链接 anydoc 引擎"
	@echo "  dev-frontend      启动前端（本地运行，需先运行 dev-start）"
	@echo ""

# Go related variables
BINARY_NAME=Reliquary
MAIN_PATH=./cmd/server

# Docker related variables
DOCKER_IMAGE=reliquary-app
DOCKER_TAG=latest

# Platform detection
ifeq ($(shell uname -m),x86_64)
    PLATFORM=linux/amd64
else ifeq ($(shell uname -m),aarch64)
    PLATFORM=linux/arm64
else ifeq ($(shell uname -m),arm64)
    PLATFORM=linux/arm64
else
    PLATFORM=linux/amd64
endif

# Build the application
build:
	go build -o $(BINARY_NAME) $(MAIN_PATH)

# Build the anydoc static archive (Rust) that the `anydoc` build tag links.
# Override the platform with TARGET=<rust-target-triple>.
anydoc-lib:
	./scripts/build-anydoc-lib.sh

# Build the application with the in-process anydoc parser engine linked in.
build-anydoc: anydoc-lib
	go build -tags anydoc -o $(BINARY_NAME) $(MAIN_PATH)

# Run the application
run: build
	VAULT_ROOT="$${VAULT_ROOT:-$(CURDIR)/.local-data/vault}" SSRF_WHITELIST_EXTRA="$${SSRF_WHITELIST_EXTRA-198.18.0.0/15}" ./$(BINARY_NAME)

reset-password:
	go run ./cmd/server reset-password

# Run tests
test:
	go test -v ./...

.PHONY: test-pg
export RELIQUARY_TEST_PG_DSN

# Run PostgreSQL integration tests against an explicitly configured test server.
test-pg:
	@test -n "$$RELIQUARY_TEST_PG_DSN" || (echo "Set RELIQUARY_TEST_PG_DSN to a PostgreSQL test connection URL"; exit 1)
	go test -count=1 -v ./...

# Clean build artifacts
clean:
	go clean
	rm -f $(BINARY_NAME)

# Build Docker image
docker-build-app:
	@echo "获取版本信息..."
	@eval $$(./scripts/get-version.sh env); \
	./scripts/get-version.sh info; \
	docker build --platform $(PLATFORM) \
		--build-arg VERSION_ARG="$$VERSION" \
		--build-arg COMMIT_ID_ARG="$$COMMIT_ID" \
		--build-arg BUILD_TIME_ARG="$$BUILD_TIME" \
		--build-arg GO_VERSION_ARG="$$GO_VERSION" \
		--build-arg WITH_ANYDOC=$${WITH_ANYDOC:-1} \
		-f docker/Dockerfile.app -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

# Build frontend Docker image
docker-build-frontend:
	./scripts/build-frontend-dist.sh
	docker build --platform $(PLATFORM) -f frontend/Dockerfile -t reliquary-ui:latest frontend/

# Build all Docker images
docker-build-all: docker-build-app docker-build-frontend

# Run Docker container (传统方式)
# Touch .env if missing — docker-compose.yml's `env_file: [.env]` is required
# for ${ENV} interpolation in builtin_models.yaml and would otherwise refuse
# to parse on fresh clones. `start-all` handles this via check_env_file; this
# direct path needs its own guard.
docker-run:
	@[ -f .env ] || ([ -f .env.example ] && cp .env.example .env || touch .env)
	docker-compose up

# 使用新脚本启动所有服务
start-all:
	./scripts/start-all.sh

# 使用新脚本仅启动Ollama服务
start-ollama:
	./scripts/start-all.sh --ollama

# 使用新脚本仅启动Docker容器
start-docker:
	./scripts/start-all.sh --docker

# 使用新脚本停止所有服务
stop-all:
	./scripts/start-all.sh --stop

# Stop Docker container (传统方式)
docker-stop:
	docker-compose down

# 从源码构建镜像相关命令
build-images:
	./scripts/build-images.sh

build-images-app:
	./scripts/build-images.sh --app

build-images-frontend:
	./scripts/build-images.sh --frontend

clean-images:
	./scripts/build-images.sh --clean

# Restart Docker container (stop, start)
docker-restart:
	@[ -f .env ] || ([ -f .env.example ] && cp .env.example .env || touch .env)
	docker-compose stop -t 60
	docker-compose up

# Database migrations
migrate-up:
	./scripts/migrate.sh up

migrate-down:
	./scripts/migrate.sh down

migrate-version:
	./scripts/migrate.sh version

migrate-create:
	@if [ -z "$(name)" ]; then \
		echo "Error: migration name is required"; \
		echo "Usage: make migrate-create name=your_migration_name"; \
		exit 1; \
	fi
	./scripts/migrate.sh create $(name)

migrate-force:
	@if [ -z "$(version)" ]; then \
		echo "Error: version is required"; \
		echo "Usage: make migrate-force version=0 (completed baseline) or version=-1 (retry after cleanup)"; \
		exit 1; \
	fi
	./scripts/migrate.sh force $(version)

migrate-goto:
	@if [ -z "$(version)" ]; then \
		echo "Error: version is required"; \
		echo "Usage: make migrate-goto version=0"; \
		exit 1; \
	fi
	./scripts/migrate.sh goto $(version)

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Install dependencies
deps:
	go mod download

# Build for production
# GO_BUILD_TAGS adds optional build tags, e.g. GO_BUILD_TAGS=anydoc to link the
# in-process office document parser (run `make anydoc-lib` first).
build-prod:
	VERSION=$$(git describe --tags --abbrev=0 2>/dev/null || echo "$${VERSION:-unknown}"); \
	COMMIT_ID=$${COMMIT_ID:-unknown}; \
	CGO_ENABLED=1 \
	CGO_CFLAGS="-Wno-deprecated-declarations" \
	CGO_LDFLAGS="$$(if [ "$$(uname)" = 'Darwin' ]; then echo '-Wl,-no_warn_duplicate_libraries'; fi)" \
	BUILD_TIME=$${BUILD_TIME:-unknown}; \
	GO_VERSION=$${GO_VERSION:-unknown}; \
	LDFLAGS="-X 'reliquary/internal/handler.Version=$$VERSION' -X 'reliquary/internal/handler.Edition=standard' -X 'reliquary/internal/handler.CommitID=$$COMMIT_ID' -X 'reliquary/internal/handler.BuildTime=$$BUILD_TIME' -X 'reliquary/internal/handler.GoVersion=$$GO_VERSION'"; \
	go build -tags "$(GO_BUILD_TAGS)" -ldflags="-w -s $$LDFLAGS" -o $(BINARY_NAME) $(MAIN_PATH)


# Fetch the spatial extension used to inspect Excel files during intake.
download_spatial:
	go run cmd/download/duckdb/duckdb.go

clean-db:
	@echo "Cleaning database..."
	@if [ $$(docker volume ls -q -f name=reliquary_postgres-data) ]; then \
		docker volume rm reliquary_postgres-data; \
	fi

# Environment check
check-env:
	./scripts/start-all.sh --check

# List containers
list-containers:
	./scripts/start-all.sh --list

# Pull latest images
pull-images:
	./scripts/start-all.sh --pull

# Show current platform
show-platform:
	@echo "当前系统架构: $(shell uname -m)"
	@echo "Docker构建平台: $(PLATFORM)"

# Development mode commands
dev-start:
	./scripts/dev.sh start $(DEV_ARGS)

dev-stop:
	./scripts/dev.sh stop

dev-restart:
	./scripts/dev.sh restart

dev-logs:
	./scripts/dev.sh logs

dev-status:
	./scripts/dev.sh status

dev-app:
	./scripts/dev.sh app

dev-frontend:
	./scripts/dev.sh frontend

