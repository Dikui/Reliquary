# 系统管理 API

[返回目录](./README.md)

| 方法   | 路径                              | 描述                   |
| ------ | --------------------------------- | ---------------------- |
| GET    | `/system/capabilities`            | 获取部署能力清单       |
| GET    | `/system/info`                    | 获取系统信息           |
| GET    | `/system/parser-engines`          | 获取解析引擎列表       |
| POST   | `/system/parser-engines/check`    | 检查解析引擎可用性     |

## GET `/system/capabilities` - 获取部署能力清单

返回当前部署版本，以及各功能模块是否已在后端注册对应路由。`supported: false` 表示 SPA 应隐藏相关入口；字段缺失或接口不可用时不应据此清空整个菜单（fail-open）。

**权限**：登录用户或有效 API Key。

**请求**:

```curl
curl --location 'http://localhost:8080/api/v1/system/capabilities' \
--header 'Authorization: Bearer <token>' \
--header 'Content-Type: application/json'
```

**响应**:

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "edition": "standard",
    "capabilities": {
      "agents": { "supported": true },
      "integrations.api": { "supported": true },
      "settings.mcp": { "supported": true },
      "settings.websearch": { "supported": true },
      "settings.sandbox": { "supported": true }
    }
  }
}
```

## GET `/system/info` - 获取系统信息

**请求**:

```curl
curl --location 'http://localhost:8080/api/v1/system/info' \
--header 'X-API-Key: sk-xxxxx' \
--header 'Content-Type: application/json'
```

**响应**:

```json
{
    "data": {
        "version": "1.2.0",
        "edition": "community",
        "commit_id": "a1b2c3d",
        "build_time": "2025-08-12T08:00:00Z",
        "go_version": "go1.21.5",
        "keyword_index_engine": "postgres",
        "db_version": "20250810_001"
    },
    "success": true
}
```

## GET `/system/parser-engines` - 获取解析引擎列表

返回 `simple`、`anydoc`、`mineru` 和 `mineru_cloud`，每项包含 `Name`、`Description`、`FileTypes`、`Available`、`UnavailableReason`。anydoc 未链接时仍显示支持的类型，并通过 `UnavailableReason` 说明启用方法。

```bash
curl 'http://localhost:8080/api/v1/system/parser-engines' \
  --header 'X-API-Key: sk-xxxxx'
```

## POST `/system/parser-engines/check` - 检查解析引擎可用性

请求体与解析设置相同，测试当前填写的 MinerU 参数，不保存配置。响应沿用引擎列表结构。

```bash
curl 'http://localhost:8080/api/v1/system/parser-engines/check' \
  --header 'X-API-Key: sk-xxxxx' \
  --header 'Content-Type: application/json' \
  --data '{"mineru_endpoint":"https://mineru.example.com"}'
```

