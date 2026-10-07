# 0001 只保留 PostgreSQL

Status: accepted
日期：2026-10-07

## 背景

上游同时支持 PostgreSQL、SQLite（Lite 单二进制）、MySQL 初始化脚本，以及 Qdrant、Milvus、Weaviate、Elasticsearch、OpenSearch、Doris、腾讯云向量库、Neo4j 等检索后端。
本项目自研的每次表结构变更都要写两份迁移（例如 PostgreSQL `000095–000098` 与 SQLite `000017–000020`）。
本项目的部署形态是服务器多人使用，2–10 人规模。

## 决策

数据库只保留 PostgreSQL，使用 ParadeDB 镜像：向量检索用 pgvector，全文检索用 pg_search，名称模糊匹配用 pg_trgm。
删除 SQLite、MySQL 和全部外部检索后端，删除 Neo4j 正文图谱（由知识上下文的对象和关系取代）。
同时删除依赖 SQLite 的 Lite 版本和桌面版。

## 后果

- 每次表结构变更只写一份迁移。
- 默认部署少了多个容器，备份只需要处理 PostgreSQL 和文件存储。
- 数据规模达到千万级向量时，需要重新评估独立向量库。对 2–10 人的信源采集场景，短期内达不到这个规模。
- 不再提供无需数据库服务的个人试用方式。
