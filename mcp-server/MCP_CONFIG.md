# 使用 uv 运行 Reliquary MCP 服务器

> reliquary 首版仅提供源码包，尚未发布到 PyPI。以下 `./mcp-server` 安装命令从仓库根目录执行。

> 更推荐使用`uv`来运行基于python的MCP服务。
>
> 本地安装：`pip install ./mcp-server`；或从仓库根目录运行 `uvx --from ./mcp-server reliquary-mcp-server`。

## 1. 安装 uv

```bash
# macOS/Linux
curl -LsSf https://astral.sh/uv/install.sh | sh

# 或使用 Homebrew (macOS)
brew install uv

# Windows
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

## 2. MCP 客户端配置

### Claude Desktop 配置

在 Claude Desktop 设置中添加:

```json
{
  "mcpServers": {
    "reliquary": {
      "args": [
        "--directory",
        "/path/Reliquary/mcp-server",
        "run",
        "run_server.py"
      ],
      "command": "uv",
      "env": {
        "RELIQUARY_API_KEY": "your_api_key_here",
        "RELIQUARY_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```

### Cursor 配置

在 Cursor 中，编辑 MCP 配置文件 (通常在 `~/.cursor/mcp-config.json`):

```json
{
  "mcpServers": {
    "reliquary": {
      "command": "uv",
      "args": [
        "--directory",
        "/path/Reliquary/mcp-server",
        "run",
        "run_server.py"
      ],
      "env": {
        "RELIQUARY_API_KEY": "your_api_key_here",
        "RELIQUARY_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```

### KiloCode 配置

对于 KiloCode 或其他支持 MCP 的编辑器，配置如下:

```json
{
  "mcpServers": {
    "reliquary": {
      "command": "uv",
      "args": [
        "--directory",
        "/path/Reliquary/mcp-server",
        "run",
        "run_server.py"
      ],
      "env": {
        "RELIQUARY_API_KEY": "your_api_key_here",
        "RELIQUARY_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```

### 其他 MCP 客户端

对于一般 MCP 客户端配置:

```json
{
  "mcpServers": {
    "reliquary": {
      "command": "uv",
      "args": [
        "--directory",
        "/path/Reliquary/mcp-server",
        "run",
        "run_server.py"
      ],
      "env": {
        "RELIQUARY_API_KEY": "your_api_key_here",
        "RELIQUARY_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```
