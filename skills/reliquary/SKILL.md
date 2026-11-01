---
name: reliquary
description: 查询、阅读、引用、收录资料并将结论写回本机 Reliquary。用户需要检索 Reliquary 的库、核对资料原文与实体关系、读取关注动态或保存 AI产出时使用。
metadata:
  tested_against: "0.15.4"
---

# Reliquary

开始使用前先完整读取 [rules.md](rules.md)。规则文件也是 MCP 握手中 instructions 的全文；按规则选库、检索、阅读、引用及确认写操作。

## 连接本机 MCP

只连接与客户端位于同一台机器的 Reliquary 后端。默认端点为 `http://127.0.0.1:8080/mcp`；如本机后端端口不同，替换端口。不要配置局域网或公网后端。

在 Reliquary 设置中创建 API Key，通过启动客户端的环境提供 `RELIQUARY_API_KEY`。两个客户端都以 Bearer 方式认证；配置中保留变量引用，不填实际 Key。启动客户端前确保它继承了该环境变量。

### Claude Code

在项目 `.mcp.json` 中合并以下配置，再启动 Claude Code 并按客户端提示启用服务：

```json
{
  "mcpServers": {
    "reliquary": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {
        "Authorization": "Bearer ${RELIQUARY_API_KEY}"
      }
    }
  }
}
```

Claude Code 启动时展开环境变量，参见 [MCP 环境变量配置](https://code.claude.com/docs/en/mcp#environment-variable-expansion-in-mcp-json)。

### Codex

执行以下命令添加服务，参数传入环境变量名称：

```sh
codex mcp add reliquary --url http://127.0.0.1:8080/mcp --bearer-token-env-var RELIQUARY_API_KEY
```

也可在 `~/.codex/config.toml` 中合并等价配置：

```toml
[mcp_servers.reliquary]
url = "http://127.0.0.1:8080/mcp"
bearer_token_env_var = "RELIQUARY_API_KEY"
```

参见 [Codex MCP 配置](https://developers.openai.com/codex/mcp/)。

## 流程索引

多步任务先调用 `reliquary_guide`，不带参数可查看可用流程。以下流程文件由任务单 #05 补全：

| 流程 | 使用场景 | 预留文件 |
|---|---|---|
| topic-brief | 主题简报 | `workflows/topic-brief.md` |
| entity-profile | 实体档案 | `workflows/entity-profile.md` |
| compare | 对比分析 | `workflows/compare.md` |
| write-report | 写回报告 | `workflows/write-report.md` |
| ingest | 收录资料 | `workflows/ingest.md` |
| review-check | 资料自检复核 | `workflows/review-check.md` |

## 参考文件索引

以下参考文件及按目标选工具、常见错误表由任务单 #06 补全：

| 参考 | 预留文件 |
|---|---|
| HTTP 认证、接口映射与错误处理 | `reference/http.md` |
| 位置、信任标记、检索降级与分页字段 | `reference/fields.md` |
