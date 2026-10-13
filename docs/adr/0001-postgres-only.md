# 0001 只保留 PostgreSQL

Status: accepted
日期：2026-10-07（2026-10-09 按 [0007](0007-personal-single-user.md) 的个人单用户场景改写）
部分被 [0014](0014-internal-knowledge-base.md) 取代（2026-10-14）。

## 背景

上游同时支持 PostgreSQL、SQLite（Lite 单二进制）、MySQL 初始化脚本，以及 Qdrant、Milvus、Weaviate、Elasticsearch、OpenSearch、Doris、腾讯云向量库、Neo4j 等检索后端。
本项目自研的每次表结构变更都要写两份迁移（例如 PostgreSQL `000095–000098` 与 SQLite `000017–000020`）。
本项目由一个人在自己的服务器上使用，通过 Web 界面访问，后台持续运行 RSS 与站点采集、文档解析和结构化抽取任务。

## 决策

数据库只保留 PostgreSQL，使用 ParadeDB 镜像：向量检索用 pgvector，全文检索用 pg_search，名称模糊匹配用 pg_trgm。
删除 SQLite、MySQL 和全部外部检索后端，删除 Neo4j 正文图谱（由知识上下文的对象和关系取代）。
同时删除依赖 SQLite 的 Lite 版本和桌面版。

2026-10-07 将 PostgreSQL 旧 `000000` 至 `000098` 迁移压缩为单一 `000000_init` 基线。现有数据库数据不保留，旧数据库必须重建，不能原地升级。后续表结构变更从 `000001` 继续编号。

保留服务端数据库而不回到 SQLite 的理由与用户人数无关：后台任务与 Web 请求并发写入，检索同时依赖向量、全文和模糊匹配三种索引，单一 PostgreSQL 能在一个事务内覆盖这些需求。

## 后果

- 每次表结构变更只写一份迁移。
- 默认部署少了多个容器，备份只需要处理 PostgreSQL 和文件存储。
- 数据规模达到千万级向量时，需要重新评估独立向量库。个人信源采集的规模短期内达不到。
- 不再提供无需数据库服务的试用方式；个人使用同样需要通过 Docker Compose 运行 PostgreSQL 与 Redis。

2026-10-11 更新：部署已按 [0010](0010-postgres-task-queue.md) 删除 Redis，后台任务与数据共用 PostgreSQL。
