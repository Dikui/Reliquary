# 实体档案与文章检索

Reliquary 在个人空间的多个库中收集、存储、去重与检索文章。网页的“实体”入口提供实体档案与待合并建议，实体按库独立，相关文章只来自实体所属库。

## 收集与文章处理

资料由外部工具通过 HTTP 收录接口或 MCP 以可读 Markdown 及可选图片送入库，Reliquary 不抓取原文、图片或文件。技术校验后先保存，再后台建立正文索引并处理；新资料不做模型准入审核，历史待审保留显式人工操作。接口见 [收录说明](ingest-api.md)。

所有库使用通用模板，后台生成内容类型、统一摘要、实体、主题和有原文依据的关系。摘要要点随实际内容确定；支持视觉的处理模型可读取已保存图片，不再默认生成逐图说明。所有内容类型均保存提交的正文。

同库只按规范化 URL、`source_ref` 或 `identifier` 精确定位来源，内容变化覆盖原 ID。没有可靠来源标识时新建，不按相同或近似正文归并转载；历史转载来源仍可读取。缺少可解析发布时间时留空，采集时间不能代替发布时间。

## 实体档案

实体类型固定为公司（`company`）、人物（`person`）、产品（`product`）、项目（`project`）、组织（`organization`）和其他（`other`）。实体详情显示类型、别名和提及该实体的文章，点击资料可读取数据库正文。自动实体抽取范围和证据要求见 [领域模型](../CONTEXT.md#language)。

收录处理已经接入实体解析服务。服务接收文章 ID 和处理模型给出的名称、类型及可选别名，先在文章所属库匹配规范名与别名，再结合本库名称召回和可用的名称向量召回，由模型判断是否属于已有实体。无法确定时建立独立实体并记录待合并建议，用户可在实体页接受或拒绝建议。非法类型按其他处理。

实体只允许在同库内合并；合并会把别名和文章关联转移到保留实体，重复关联会去重，并保存可查询的合并记录。相关文章的表头随合并更新，合并操作写入操作日志。删除文章会清理关联，实体档案继续保留。

## 人工维护产品库

新建普通库后，在库设置将实体模式改为“人工维护”，设置类别、属性与关系规则。只有完全空库可切换模式。实体页提供新建、编辑、归档、恢复与修改记录；资料详情提供人工关联与排除。

此模式下自动处理只按名称、代码或别名唯一匹配已有实体。文档不会创建或更新产品、属性、别名和关系。未匹配项可以人工补关联。重新处理保留人工关联及排除，归档实体从默认目录与图谱隐藏，历史资料仍可读取。实体名称与非空代码在库内唯一。

HTTP 接口位于 `/api/v1`，成功数据在 `data` 中。模式、类别和属性定义使用现有 `POST /libraries` 或 `PUT /libraries/:id`；关系类型定义使用后者。

| 接口 | 请求或结果 |
| --- | --- |
| `POST /libraries/:id/entities` | `{name, kind, code, description, aliases:[], attributes:{}}`，返回实体 |
| `PATCH /entities/:entity_id` | 必填当前 `revision`；其余字段可选，`attributes` 整体替换；`archived:true/false` 归档或恢复 |
| `GET /entities?library_id=...` | 可增加 `kind`、`include_archived=true`，沿用 limit/offset |
| `GET /entities/:entity_id/history` | `page`、`page_size`，返回操作日志分页，修改前后值在 `before`、`after` |
| `GET /entries/:id/entity-links` | 包含人工排除项，返回 entity、origin、excluded、evidence、body_hash |
| `PUT /entries/:id/entity-links/:entity_id` | `{excluded:false}` 补关联或恢复；`{excluded:true}` 排除 |

库的 `entity_schema` 格式为 `{kinds:[{key,name,parent_kinds:[]}],fields:[{key,name,type,unit,options:[],kinds:[],archived:false}]}`。key 使用小写英文字母开头的字母、数字、下划线，最长 64 字符。字段 type 为 text、number、boolean、date（YYYY-MM-DD）或 select。fields 的 kinds 为空表示适用全部类别；parent_kinds 为空表示不允许父项。选项或字段定义变更不能使现有值失效。已有值的类型与单位不能直接变更。

字段值不存在时从 attributes 中删除该键，不存 null。已归档字段保留历史值但不能修改。实体 revision 或关系 version 冲突返回 409，重新读取后再修改；不自动覆盖。关系维护见[关系接口](relations-api.md#人工维护库)。

## 名称向量

空间共享的向量模型决定实体名称向量的模型与维度。实体页可触发名称向量重建并查看维护任务状态。没有可用嵌入模型时仍可按名称和别名匹配。

## 文章检索与文件库

知识检索使用全文与向量混合检索，覆盖文章表头、全文正文、用户文档和整理结果，按文章合并命中片段。可按内容类型、实体、主题、发布时间、星标和来源目录筛选。发布时间筛选排除缺少发布时间的文章并说明数量，相似报道可以折叠和展开。

正文保存在 PostgreSQL，图片等原始资源保存在磁盘。显式导出文件库时才按各库实际英文目录生成 Markdown 和数据库表头；导出文件可用 Obsidian 离线阅读。正文只在显式更新或同来源覆盖时改变，不保留旧版本。综合分析由外部 AI 通过检索、原文读取与收录接口完成。

## 数据库与配置

生产数据库使用 PostgreSQL（ParadeDB），保存文章记录、检索索引、实体档案、主题、用户判断和操作日志，正文保存在数据库，附件保存在磁盘。旧结构需要重新安装，具体操作见[数据库迁移排障](./migration-troubleshooting.md#single-baseline-and-database-rebuild)。当前版本搬迁到另一台机器时，一起复制文件库、导出导入数据库并修改配置。

各库独立配置处理模型 `generation`、实体判断模型 `entity_resolution` 和重排模型；正文与实体名称向量使用空间共享的向量配置。安装、认证、文件库、SSRF 配置、备份恢复和 MCP 接入见[个人使用指南](./personal-use.md)。
