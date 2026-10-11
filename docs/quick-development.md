# 快速开发模式

解决开发流程中，每次修改 `app`（后端）或 `frontend`（前端）代码后，都需要打包 Docker 镜像的问题，实现这两个模块的热更新。

> 本页是 [开发指南](development.md) 的补充，提供更详细的架构说明

## 使用方法

### 方式 1：Make 命令（推荐）

```bash
make dev-start       # 终端 1：启动基础设施
make dev-app         # 终端 2：启动后端
make dev-frontend    # 终端 3：启动前端
```

### 方式 2：开发脚本

```bash
./scripts/dev.sh start      # 终端 1
./scripts/dev.sh app         # 终端 2
./scripts/dev.sh frontend    # 终端 3
```

### 方式 3：一键启动

```bash
./scripts/quick-dev.sh
```

### 使用 Air 实现后端热重载

```bash
go install github.com/air-verse/air@latest
export PATH="$PATH:$(go env GOPATH)/bin"
make dev-app  # 自动检测 Air 并使用
```

## 架构对比

### 开发模式

```
本地前端 UI (:5173) → 本地后端 App (:8080) → PostgreSQL 容器
```

- 后端/前端在本地直接运行，修改后秒级生效
- 基础设施仍使用 Docker 容器

### 生产模式

```
容器前端 UI (:80) → 容器后端 App (:8080) → PostgreSQL 容器
```

- 所有服务均运行在 Docker 容器中
- 每次修改需要重新构建镜像

## 效率对比

| 方式 | 每次修改耗时 |
|------|------------|
| 旧方式（重建镜像） | 2-5 分钟 |
| 新方式（本地开发） | 后端 5-10 秒，前端实时热重载 |

完整工作流、脚本职责和集成测试见[开发指南](development.md)。
