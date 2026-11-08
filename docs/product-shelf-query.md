# 货架查询接口

界面与外部 agent 使用同一组 `EntityService.ShelfFind`、`ShelfGet`、`ShelfQuery` 读取服务。HTTP 路径位于 `/api/v1`，沿用本机访问与 API Key 规则，响应包装为 `{"success":true,"data":...}`。本组接口不写业务数据；写入使用[货架变更集](product-shelf-changesets.md)。

MCP 分别提供 `shelf_find`、`shelf_get`、`shelf_query`，均为只读工具。三个工具必传 `library_id`；find 的其他参数为 `q`、`limit`、`offset`，get 为 `node_id`、可选 `combination` 对象和 `as_of`，query 的其他参数与下述 JSON 筛选相同。返回与 HTTP 的 `data` 相同，同时提供结构化结果与 JSON 文本。调用前可通过 `reliquary_guide` 的 `shelf-expert` 流程读取[货架专家说明](../skills/reliquary/workflows/shelf-expert.md)。

## 状态表导出 Excel

`POST /libraries/:id/shelf/export`

```json
{
  "product_line_id": "product-line-id",
  "query": {"sale_statuses": [{"region": "region_key", "status": "GA"}]}
}
```

返回可下载的 `.xlsx` 文件，不包装 JSON。`product_line_id` 可省略，表示全部活动产品线；指定时必须是本库的活动产品线。每条产品线一个工作表，包括无型号或筛选无结果的产品线。没有活动产品线时返回 400。

`query` 复用型号筛选与可选 `as_of`，省略表示不筛选。产品线范围以 `product_line_id` 为准，忽略 `query` 中的 `ancestor_id`、`limit`、`offset`，始终导出全部匹配型号。所有工作表在同一数据库快照和同一业务时点读取。筛选只选择型号，单元格保留该型号的完整区域记录及其适用条件。

每个工作表首行为产品线原名，第二行为产品平台、产品名称、产品型号、描述及该产品线出现过记录的区域，区域沿用库配置顺序与原名，筛选不缩减区域列。型号按平台、产品名称、型号分组排序。无记录保持空白，明确的待定保留为待定；同一区域的多条条件记录分行显示，带模块和部件名称、备注与原始写法。计划退市同时显示文字标记和黄色填充，不从 EOM 推断计划退市。

Excel 工作表名称的非法字符、长度、保留名称及重名按格式约束调整，表内产品线原名和所有节点名称保留。所有单元格按文本保存，类似公式的客户写法不会执行。单元格超过 Excel 的 32767 个 UTF-16 单元限制时返回 400，不截断原文。

网页导出菜单支持当前页签、全部产品线，以及是否应用当前已生效筛选。下载不受勾选行、折叠状态或分页限制，不写入货架数据。

## 找节点

`GET /libraries/:id/shelf/find?q=名称&limit=50&offset=0`

`q` 必填，按名称、明确别名或代码做不区分大小写的字面包含匹配。名称与别名沿用实体名称规范化；`%` 和 `_` 不作为通配符。返回 `items`、分页前的 `total` 和 `truncated`。同名保留全部候选，候选带 `id`、`kind` 和 `kind_label`，调用方不能自动选择一个类别。

候选按当前名称、类别、ID 稳定排序，默认隐藏归档与已合并实体。候选中的 `attributes` 为空；属性事实应通过 get 读取。

## 读节点

`GET /libraries/:id/shelf/nodes/:node_id?as_of=2026-11-07T00:00:00Z&combination={"module_key":"part_id"}`

`as_of` 为可选 RFC 3339 时间，默认当前时间。`combination` 为可选的模块键到部件 ID 的 JSON 对象，放在 URL 时需要编码。指定组合时须选择每个选配模块，固定部件可省略。未指定组合时保留全部适用条件。组成列表保留可选部件供切换，事实列表只显示选中组合适用的部件属性与组成关系。

响应包含：

- `entity`：实体身份及指定时间的属性，带类别标签。名称、类别、代码与归档状态遵循 ADR 0022，使用当前身份信息，不假装有历史名称版本。
- `as_of`：实际读取的业务时间。
- `recorded`：该时间是否已有属性快照。早于最早记录时为 `false`，属性和事实为空，不把当前值反推到过去。
- `shelf`：复用实体详情中的组成、区域状态、参数和认证视图。
- `facts`：附带证据与适用条件的事实，供界面展示和 agent 引用。

每条事实包含 `entity_id`、`field`、`value`、`conditions`、`partial_options`、`origin`、`origin_label`、`evidence`。本组接口中的关系事实包含所属层级、组成和适用认证归属，另带 `relation_id`。参数值按字段逐项返回，区域状态按区域与条件逐项返回。部件认证的有效条件包含其组成部件和电芯限制，即使原认证没有重复填写显式条件。

`origin` 为 `self`、`component` 或 `ancestor`，中文标签为“自身”“来自部件”“来自上层”。单部件型号复用部件认证并标为自身。未选择组合时，只有部分选配适用的事实标记 `partial_options=true`；选择组合后，返回的事实均适用于该组合。

`evidence` 包含业务生效时间 `effective_at`、记录时间 `recorded_at`，以及可追溯的 `snapshot_id` 或关系 ID、`operation_log_id`、`changeset_id` 和提交来源 `source`。来源沿用变更集的网页、具名 agent、导入批次和文件日期。查询不会生成原数据中没有的工作表或单元格信息。未改变的完整字段追溯到其原快照；历史快照被回填时追溯对应的修改日志。旧记录没有来源日志时仍提供快照与时间，来源保持空值。

## 筛选与反查型号

`POST /libraries/:id/shelf/query`

```json
{
  "ancestor_id": "product-line-or-platform-or-product-id",
  "sale_statuses": [{"region": "region_key", "status": "GA"}],
  "parameters": [{"field": "energy", "min": 5, "max": 60}],
  "certification": {
    "standard": "已配置的标准键、规范名或明确别名",
    "progress": "提交方填写的进度",
    "region": "提交方填写的地区"
  },
  "composition": [{"module_type": "module_key", "part_ids": ["part-id"]}],
  "as_of": "2026-11-07T00:00:00Z",
  "limit": 50,
  "offset": 0
}
```

所有筛选项均可省略，默认返回当前活动型号。`ancestor_id` 限定所属层级范围，也允许直接指定型号本身。多个区域、参数和组成条件为合取，必须存在同一个部件组合同时满足全部条件。认证的标准、进度和地区必须由同一条适用认证记录满足，包含来自部件和来自上层的认证。查询按业务时间使用属性快照与关系有效区间，不展开或持久化完整组合实体。

数值筛选至少提供 `min` 或 `max`，采用闭区间相交语义。单值等同于上下界相同的区间。例如参数为 `[9,54]` 时会命中 `min=50`，响应保留原范围，调用方不能把它说成保证达到 50。参数必须是已配置的产品线数值字段。

标准按规范名、明确别名或键匹配；未知标准返回零条。进度与认证地区按原写法精确匹配，显式空字符串用于查找未填写值。区域状态的区域键来自货架配置，认证地区不映射为区域状态。

状态表也可省略状态筛选项的 `region` 表示任一区域，或只给 `region` 表示该区域的任意已记录状态。`planned_eom:true/false` 筛选计划退市标记，仍与其他条件在同一选配组合上求交。`no_record:true` 表示指定区域完全没有记录；省略区域时表示没有任何区域记录，不表示任意一格为空，也不把部分选配无记录推断为整格无记录。`no_record` 不能与 `status` 或 `planned_eom` 同时指定，空筛选项会被拒绝。

反查复用同一接口：`{"part_id":"part-id"}` 返回使用该部件的型号；`{"certification":{"standard":"标准写法"}}` 返回具有适用认证的型号。`composition` 可按模块指定多个可接受部件。部件归档不会删除已有组成关系，反查与型号详情仍保留这些已记录的使用关系；活动型号与认证清单默认隐藏自身已归档的条目。

响应包含 `items`、`total`、`truncated`、`as_of`。`total` 为分页前的型号数，不因同型号多条匹配认证而重复计数。每页默认 50 条，上限 100；`offset` 为非负数。只要本次返回的条目没有覆盖全部结果，`truncated` 就为 `true`，包括非第一页和越界空页。结果按当前型号名称与 ID 稳定排序。

每个结果含 get 的完整节点详情，并增加 `matching_conditions` 和型号级 `partial_options`。`matching_conditions` 的外层为“或”，每组内部为“且”，同模块重复条件取交集。只有全部选配均能满足筛选时，型号级 `partial_options` 才为 `false`；多条认证分别覆盖不同选配，也可以合起来覆盖全部选配。完整详情仍保留其他事实及其条件，引用命中原因时使用 `matching_conditions`。

这三个接口只提供货架中已记录的事实。GA 不代表认证齐全或可直接销售。没有记录时应明确说明“货架中没有记录”。资料清单和汇总见下文；节点范围全文检索由对应检索接口提供。

## 资料清单与汇总

get 与 query 返回的 `shelf` 包含型号的 `current_stage`、`stage_owner_id` 与 `checklist`。产品名称、平台和产品线返回 `checklist_summary`。普通实体详情也复用同一计算。

型号应有项是所属产品线要求与组成中各模块类型要求的并集。始终使用当前活动资料要求的最新有效字段与适用关系，不按型号创建日期固定旧清单。上层与组成关系、型号和产品名称阶段按 `as_of` 读取；资料版本按 `occurred_at <= as_of` 中最大的文件内顺序号读取。云盘链接与本机原件均能满足要求，解析状态不影响交付状态。

每条清单项包含 `requirement`（身份、完整字段及来源清单版本）、`document`（选中的版本及来源与时间）、`has_file`、`owner_id`、`origin`、`origin_label`、`conditions`、`partial_options`、`status`、`status_label`。查找优先级为型号、部件、产品名称、平台。较近的初稿会遮住较远的终稿，旧版本不会代替当前初稿。部件文件隐含所属部件的组合条件。

有阶段时，状态依次判断：

| 条件 | status | 显示 |
| --- | --- | --- |
| 有终稿 | `delivered` | 已交 |
| 已到终稿应交阶段 | `missing` | 缺 |
| 有初稿 | `draft_only` | 只有初稿 |
| 已到初稿应交阶段 | `missing` | 缺 |
| 其余 | `not_due` | 未到期 |

未填当前阶段时只返回 `has_file`（有文件）或 `no_file`（无文件），不判断到期。阶段名和顺序均取自库配置。未指定组合时，同一要求可以按不同组合分为多行，每行保留适用条件；选择组合后只保留适用行。多个同层部件文件同时适用时保留全部候选，并用 `ambiguous=true` 标识存在重叠，由提交方确认归属或条件，不按内部 ID 猜选一份。

`checklist_summary` 包含 `model_count`、`models_with_gaps`、按状态汇总的 `counts`，以及每个型号的 ID、当前阶段、状态计数与 `has_gaps`。仅汇总该业务时间已有记录的当前活动下属型号。计数以“型号、要求、状态”为单位，同状态的多组选配条件不重复计数；不同状态分别保留，避免隐去部分选配缺失。

query 可加 `{"document_gaps":true}` 筛选有 `missing` 项的型号。缺口条件与区域、参数、认证及组成筛选在数据库中求同一可行组合，保留分页前总数和部分选配标记。只有初稿、未到期与未填阶段的无文件不直接判作到期缺口；省略或传 `false` 不启用该筛选。
