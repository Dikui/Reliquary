# 向量库状态 API

[返回目录](./README.md)

检索统一使用 PostgreSQL（ParadeDB 镜像，pgvector + pg_search）。设置页只读显示 PostgreSQL 状态，不提供创建、修改、删除或切换引擎的入口。

## GET `/vector-stores`

返回部署的 PostgreSQL 存储状态。需要当前空间的查看权限，响应不包含数据库凭据。

```sh
curl 'http://localhost:8080/api/v1/vector-stores' -H 'X-API-Key: sk-xxxxx'
```

返回数据包含 `id`、`name`、`engine_type`、`source`、`status` 和只读标记。`engine_type` 固定为 `postgres`；连接可用性根据服务端的 PostgreSQL 连接检测。

知识库仍保留既有 `vector_store_id` 和显示投影字段，供知识上下文模块使用；新建或更新知识库不会选择其他检索引擎。
