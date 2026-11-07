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

新建普通库后，在库设置将实体模式改为“人工维护”，设置类别、属性与关系规则。已有文献的库可以在显式映射已有实体类别、解决名称冲突后转换。实体页提供新建、编辑、归档、恢复与修改记录；资料详情提供人工关联与排除。

此模式下自动处理只按名称、代码或别名唯一匹配已有实体。文档不会创建或更新产品、属性、别名和关系。未匹配项可以人工补关联。重新处理保留人工关联及排除，归档实体从默认目录与图谱隐藏，历史资料仍可读取。活动名称、别名和代码在每个类别内共用唯一名称空间，不受原存储类型影响。跨类别同名须确认具体实体后保存，确认结果持久化；列表和详情包含 `kind` 与 `kind_label`。名称原样保存，比较沿用名称规范化规则。归档后名称和别名可复用；非空代码不区分大小写，归档仍保留类别内的代码占用。没有未排除文献关联、关系或其他受保护引用的产品可以删除；只剩排除记录的产品也可删除，排除记录一并清理。关系对端、合并来源、产品引用与文献关联的选择器均按名称在服务端搜索。

HTTP 接口位于 `/api/v1`，成功数据在 `data` 中。模式、类别和属性定义使用现有 `POST /libraries` 或 `PUT /libraries/:id`；关系类型定义使用后者。

| 接口 | 请求或结果 |
| --- | --- |
| `POST /libraries/:id/entities` | `{name, kind, code, description, aliases:[], attributes:{}}`，返回实体 |
| `PATCH /entities/:entity_id` | 必填当前 `revision`；其余字段可选，`attributes` 整体替换；`archived:true/false` 归档或恢复 |
| `GET /entities?library_id=...` | 可增加 `kind`、`include_archived=true`，沿用 limit/offset |
| `GET /entities/:entity_id/history` | `page`、`page_size`，返回操作日志分页，修改前后值在 `before`、`after` |
| `GET /entries/:id/entity-links` | 包含人工排除项，返回 entity、origin、excluded、evidence、body_hash |
| `PUT /entries/:id/entity-links/:entity_id` | `{excluded:false}` 补关联或恢复；`{excluded:true}` 排除 |

货架通过 `POST /libraries`（同时指定 `entity_mode: "managed"`）或 `PUT /libraries/:id` 的 `shelf_enabled: true` 开启。服务端替换未使用的旧配置，写入八个固定类别以及 `belongs_to`、`composed_of`、`applies_to`、`has_certification`、`has_document`、`fulfills` 关系类型。已有不兼容实体、属性或关系时拒绝开启并保留数据，不推断迁移。开启后不能关闭货架或修改固定的层级和关系端点；可继续定义实体与关系字段。

库的 `shelf_config` 保存 `module_types`、`regions`、`stages` 三个列表。每项为 `{ "key": "stable_key", "name": "原样名称" }`，各列表最多 100 项，名称最多 100 字符。更新时提交完整 `shelf_config`，保留已有键，可追加、改名和重排；阶段按数组从前到后推进。空列表可省略。`GET /libraries` 和 `GET /libraries/:id` 返回持久化配置，各库独立。

货架的 `product_library_ids` 必须为空，也不能出现在其他库的自动匹配目标中。开启前须先从其他库的目标列表移除。后台处理会再次过滤货架，保留人工关联和排除决定；普通人工维护库的名称匹配行为不变。

同类别的名称、别名或代码冲突返回 409，不能用确认跳过。跨类别同名也返回 409，`error.details.type` 为 `cross_kind_name_confirmation_required`；`conflicts` 逐项给出提交的写法与类别，以及另一实体的 `other_entity_id`、`other_name`、`other_kind` 和 `other_kind_label`。确认具体对象后，在原请求增加：

```json
{
  "name_confirmations": [
    {"name": "Shared Product", "other_entity_id": "existing-entity-id", "other_kind": "platform"}
  ]
}
```

创建、修改、恢复与合并均支持此字段；拆分放在 `new_entity` 中。同一对实体、类别与规范化写法已经确认时，不必再次提交确认。新对象或类别变化产生的新冲突仍须确认。确认与本次写入处于同一事务，写入失败不保留确认。确认记录随文件库显式导出和恢复保存。

已有库转换也使用此校验，类别取自 `entity_kind_mapping`。转换请求的每条 `name_confirmations` 还须提供已有的 `entity_id`，指定被确认的一端；另一端仍由 `other_entity_id` 与 `other_kind` 指定。转换冲突中的 `entity_id` 可用于构造该确认。

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
