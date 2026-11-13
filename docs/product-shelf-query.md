# 货架查询接口

界面与外部 agent 使用同一组 `EntityService.ShelfFind`、`ShelfGet`、`ShelfQuery` 读取服务。HTTP 路径位于 `/api/v1`，沿用本机访问与 API Key 规则，响应包装为 `{"success":true,"data":...}`。本组接口不写业务数据；写入使用[货架变更集](product-shelf-changesets.md)。

MCP 分别提供 `shelf_find`、`shelf_get`、`shelf_query`，均为只读工具。三个工具必传 `library_id`；find 的其他参数为 `q`、`limit`、`offset`，get 为 `node_id`、可选 `combination` 对象和 `as_of`，query 的其他参数与下述 JSON 筛选相同。返回与 HTTP 的 `data` 相同，同时提供结构化结果与 JSON 文本。调用前可通过 `reliquary_guide` 的 `shelf-expert` 流程读取[货架专家说明](../skills/reliquary/workflows/shelf-expert.md)。

## 补充规范

不传 `node_id` 时另返回 `import_fields`，按文件种类列出可映射列、键、必填及工作表字段，与方案校验同源；字段说明见[导入方案](product-shelf-import.md#导入方案)。节点请求省略此元数据。

维护货架前调用只读 MCP 工具 `shelf_guide`，必传 `library_id`，可选 `node_id`。本工具只通过 MCP 提供，与其他货架工具一样同时返回结构化 JSON 和 JSON 文本。

返回 `library_id`、`library_updated_at`、`as_of`、`max_file_bytes`、`rules`、`values`、`import_plans`，指定节点时另带 `gaps`。所有动态内容在同一只读数据库快照中读取，固定规则来自[嵌入的补充规范](../skills/reliquary/reference/shelf-supplement.md)。工具不保存提议或修改配置。

`values` 包含当前库的类别及父类别 `kinds`、活动产品线 `product_lines`、区域 `regions`（键、原显示名及可选 `aliases`）、独立市场 `markets`、按数组顺序排列的阶段 `stages`、部件类型 `module_types`、标准及明确别名 `standards`、固定可售状态 `sale_statuses`、资料要求清单汇总 `requirement_lists` 和库内字段定义 `fields`。名称保留原写法，键和 ID 用于提交。

`requirement_lists` 每项为 `{name,total,product_lines:[{id,name}]}`，按所属清单原名汇总当前活动要求，产品线按有效 applies_to 绑定去重，没有绑定时返回空数组。不传 node_id 时不返回逐条 `requirements`。指定节点时，`values.requirements` 为 `{items,total,truncated}`：型号取其清单要求，上层节点取全部下属型号涉及的要求并按 ID 去重，部件或没有下属型号的节点返回空列表。每项仅含 id、name（原实体名）、list_name、name_zh、name_en、draft_due_stage、final_due_stage；空的中英文名与初稿阶段可缺省，不返回完整实体属性。按原实体名及 ID 排序，完整计数后最多保留 100 项；截断后可用 shelf_get 的型号清单核对剩余要求。

`rules` 说明 shelf_submit 各操作必填项、具名 agent 来源、先预览经用户同意再确认、含义不确定项的修正或放弃或暂缓。资料原件先用有效 API Key 上传至 `POST /api/v1/libraries/:id/entity-attachments`（multipart 的 file），再经 add_document_version 变更集归类。MCP 不能传二进制，上传暂存不确认货架关联。单文件上限使用实际生效的 max_file_bytes；owner_id 层级、requirement_ids、draft/final、版本、unassigned、证书编号及向下共用规则均在规范中说明。

`import_plans` 复用已保存方案查询，包含 key、file_kind、definition 中各工作表、header_rows、列字母与字段映射，以及 last_file_name、last_file_date、last_headers。日期为最近批次的文件业务日期，表头为最近文件中映射列的原文，多行以换行连接；历史批次未记录时缺省，不推断。方案修订后仍须核对表头。Excel 用 API Key 上传 `POST /api/v1/libraries/:id/shelf/imports`，后续通过 shelf_submit 审核。同内容、方案修订和生效时间重复上传复用原变更集；消失的行不自动删除，与手工值不同列为冲突。

`gaps` 的节点必须是本库产品线、平台、产品名称、型号或部件。型号及上层节点返回 model_count，以及四个独立的 `{items,total,truncated}` 列表：

- documents：现有清单中的 missing 和 draft_only 行，保留 model_id、model_name、current_stage，以及原 requirement（含 ID、所属清单、初稿及终稿应交阶段）、条件、部分选配、所选版本和状态。未到期项不列入。
- no_file_documents：现有清单中的 no_file（无文件）行，保留与 documents 相同的型号、要求、条件和状态字段，独立计数，不推断已经到期。每行 current_stage_unset=true 表示型号和产品名称均未设置当前阶段；型号节点的 gaps 也会返回 current_stage_unset=true，提醒先补阶段。型号设置或从产品名称沿用阶段时不提示未设置。
- sale_statuses：本库 regions 中该型号完全没有记录的区域，包含型号和区域的键及原名；已有条件记录的区域不算整区无记录。
- pending_changesets：涉及节点、下属型号或其共用上层、组成部件、当前要求、认证、文件的待确认变更集摘要（id、revision、source、updated_at），按变更集去重。明确的关系两端、候选、决定和适用条件引用也参与判断；库设置提议影响整个库。原操作和冲突可用变更集 HTTP GET 读取。

上层节点通过现有查询服务遍历全部当前活动下属型号，按同一 as_of 汇总，再分别截取各列表前 100 项。documents 与 no_file_documents 分别计数和截断。total 是截断前条数：资料按清单条件行、区域按型号与区域、变更集按唯一 ID 计数。部件 model_count 为 0，两类资料与区域列表为空，只返回涉及它的待确认变更集。截断后可用 shelf_query、shelf_get 逐个型号查看，待确认列表用 HTTP 分页继续读取；不把截断列表当成全部。

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

每个工作表首行为产品线原名，第二行为产品平台、产品名称、产品型号、描述及该产品线出现过记录的区域，区域沿用库配置顺序与原名，筛选不缩减区域列。型号按平台、产品名称、型号分组排序。无记录保持空白，明确的待定保留为待定；同一区域的多条条件记录分行显示，带部件类型和部件名称、备注与原始写法。计划退市同时显示文字标记和黄色填充，不从 EOM 推断计划退市。

Excel 工作表名称的非法字符、长度、保留名称及重名按格式约束调整，表内产品线原名和所有节点名称保留。所有单元格按文本保存，类似公式的客户写法不会执行。单元格超过 Excel 的 32767 个 UTF-16 单元限制时返回 400，不截断原文。

网页导出菜单支持当前页签、全部产品线，以及是否应用当前已生效筛选。下载不受勾选行、折叠状态或分页限制，不写入货架数据。

## 找节点

`GET /libraries/:id/shelf/find?q=名称&limit=50&offset=0`

`q` 必填，按名称、明确别名或代码做不区分大小写的字面包含匹配。名称与别名沿用实体名称规范化；`%` 和 `_` 不作为通配符。返回 `items`、分页前的 `total` 和 `truncated`。同名保留全部候选，候选带 `id`、`kind` 和 `kind_label`，调用方不能自动选择一个类别。

候选按当前名称、类别、ID 稳定排序，默认隐藏归档与已合并实体。候选中的 `attributes` 为空；属性事实应通过 get 读取。

## 读节点

`GET /libraries/:id/shelf/nodes/:node_id?as_of=2026-11-07T00:00:00Z&combination={"module_key":"part_id"}`

`as_of` 为可选 RFC 3339 时间，默认当前时间。`combination` 为可选的部件类型键到部件 ID 的 JSON 对象，放在 URL 时需要编码。指定组合时须选择每个选配部件类型，固定部件可省略。未指定组合时保留全部适用条件。组成列表保留可选部件供切换，事实列表只显示选中组合适用的部件属性与组成关系。

响应包含：

- `entity`：实体身份及指定时间的属性，带类别标签。名称、类别、代码与归档状态遵循 ADR 0022，使用当前身份信息，不假装有历史名称版本。
- `as_of`：实际读取的业务时间。
- `recorded`：该时间是否已有属性快照。早于最早记录时为 `false`，属性和事实为空，不把当前值反推到过去。
- `shelf`：复用实体详情中的组成、区域状态、参数和认证视图。
- `facts`：附带证据与适用条件的事实，供界面展示和 agent 引用。

每条事实包含 `entity_id`、`field`、`value`、`conditions`、`partial_options`、`origin`、`origin_label`、`evidence`。本组接口中的关系事实包含所属层级、组成和适用认证归属，另带 `relation_id`。参数值按字段逐项返回，区域状态按区域与条件逐项返回。部件认证的有效条件包含其组成部件和电芯限制，即使原认证没有重复填写显式条件。

`origin` 为 `self`、`component` 或 `ancestor`，中文标签为“自身”“来自部件”“来自上层”。单部件型号复用部件认证并标为自身。未选择组合时，只有部分选配适用的事实标记 `partial_options=true`；选择组合后，返回的事实均适用于该组合。

`evidence` 包含业务生效时间 `effective_at`、记录时间 `recorded_at`，以及可追溯的 `snapshot_id` 或关系 ID、`operation_log_id`、`changeset_id` 和提交来源 `source`。来源沿用变更集的网页、具名 agent、导入批次和文件日期。查询不会生成原数据中没有的工作表或单元格信息。未改变的完整字段追溯到其原快照；历史快照被回填时追溯对应的修改日志。旧记录没有来源日志时仍提供快照与时间，来源保持空值。

可售状态按区域追溯来源。网页或 agent 提交整组 `sale_statuses` 时，仅状态、计划退市、备注、原始写法或适用条件实际改变的区域采用新来源；未变区域保留原快照、导入文件、工作表、单元格、变更集与操作日志。区域记录与条件集合的排列变化，以及无条件的 `null` 与空数组差异，不算区域事实改变。同一区域的条件记录共用该区域的来源，按组合筛选不改变来源。删除后重新新增的区域从重新新增时追溯，不复用删除前的来源。

以上规则同时适用于 `as_of` 读取和已有历史数据，无需改写已保存的快照或日志。型号页在各区域下显示独立来源，可展开查看工作表、单元格、变更集与生效和记录时间。

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
    "region": "region_key"
  },
  "composition": [{"module_type": "module_key", "part_ids": ["part-id"]}],
  "as_of": "2026-11-07T00:00:00Z",
  "detail": "summary",
  "limit": 50,
  "offset": 0
}
```

`detail` 控制每项详略，默认 `summary`，响应顶层 `detail` 回显本次取值。`summary` 每项只含 `entity`（保留名称、类别、代码、别名与 `description`，`attributes` 置空）、`current_stage`、`recorded`、`as_of`、`matching_conditions`、`partial_options`，以及本次筛选检查过的 `facts`：所筛区域的可售状态（未给区域时为全部区域）、所筛参数字段的取值、满足认证条件的认证；每条事实保留条件、来自部件或上层标记与来源证据。不返回 `shelf`，即资料清单、其他文件、组成、完整认证与参数表。`full` 每项返回与 `GET /shelf/nodes/:node_id` 相同的完整详情，体积随型号和资料数线性增长，只在确需逐项资料清单时少量使用。只要数量时传 `limit=1` 读 `total`。货架导出与节点缺口在服务内部读取完整详情，不受此参数影响。

所有筛选项均可省略，默认返回当前活动型号。`ancestor_id` 限定所属层级范围，也允许直接指定型号本身。多个区域、参数和组成条件为合取，必须存在同一个部件组合同时满足全部条件。认证的标准、进度和地区必须由同一条适用认证记录满足，包含来自部件和来自上层的认证。查询按业务时间使用属性快照与关系有效区间，不展开或持久化完整组合实体。

数值筛选至少提供 `min` 或 `max`，采用闭区间相交语义。单值等同于上下界相同的区间。例如参数为 `[9,54]` 时会命中 `min=50`，响应保留原范围，调用方不能把它说成保证达到 50。参数必须是已配置的产品线数值字段。

标准按规范名、明确别名或键匹配；未知标准返回零条。进度按原写法精确匹配。认证地区 `certification.region` 按本库配置区域键精确筛选，非空名称、别名或未知键会被拒绝；显式空字符串只匹配不分地区的认证，省略地区则不限。认证地区与可售状态使用同一套区域键，认证不推导可售状态。

状态表也可省略状态筛选项的 `region` 表示任一区域，或只给 `region` 表示该区域的任意已记录状态。`planned_eom:true/false` 筛选计划退市标记，仍与其他条件在同一选配组合上求交。`no_record:true` 表示指定区域完全没有记录；省略区域时表示没有任何区域记录，不表示任意一格为空，也不把部分选配无记录推断为整格无记录。`no_record` 不能与 `status` 或 `planned_eom` 同时指定，空筛选项会被拒绝。

反查复用同一接口：`{"part_id":"part-id"}` 返回使用该部件的型号；`{"certification":{"standard":"标准写法"}}` 返回具有适用认证的型号。`composition` 可按部件类型指定多个可接受部件。部件归档不会删除已有组成关系，反查与型号详情仍保留这些已记录的使用关系；活动型号与认证清单默认隐藏自身已归档的条目。

响应包含 `items`、`total`、`truncated`、`as_of`。`total` 为分页前的型号数，不因同型号多条匹配认证而重复计数。每页默认 50 条，上限 100；`offset` 为非负数。只要本次返回的条目没有覆盖全部结果，`truncated` 就为 `true`，包括非第一页和越界空页。结果按当前型号名称与 ID 稳定排序。

每个结果按 `detail` 返回摘要或 get 的完整节点详情，两种都增加 `matching_conditions` 和型号级 `partial_options`。`matching_conditions` 的外层为“或”，每组内部为“且”，同一部件类型重复条件取交集。只有全部选配均能满足筛选时，型号级 `partial_options` 才为 `false`；多条认证分别覆盖不同选配，也可以合起来覆盖全部选配。`full` 仍保留其他事实及其条件，`summary` 只保留命中事实；引用命中原因时使用 `matching_conditions`。

这三个接口只提供货架中已记录的事实。GA 不代表认证齐全或可直接销售。没有记录时应明确说明“货架中没有记录”。资料清单和汇总见下文；节点范围全文检索由对应检索接口提供。

## 资料清单与汇总

get 与 query 返回的 `shelf` 包含型号的 `current_stage`、`stage_owner_id` 与 `checklist`。产品名称、平台和产品线返回 `checklist_summary`。普通实体详情也复用同一计算。

型号应有项只取所属产品线的资料要求。部件类型不带出要求，部件没有自身应交资料清单，但部件上的文件仍可按原规则供型号共用。始终使用当前活动资料要求的最新有效字段与适用关系，不按型号创建日期固定旧清单。上层与组成关系、型号和产品名称阶段按 `as_of` 读取；资料版本按 `occurred_at <= as_of` 中最大的文件内顺序号读取。云盘链接与本机原件均能满足要求，解析状态不影响交付状态。

每条清单项包含 `requirement`（身份、完整字段及来源清单版本）、`document`（选中的版本及来源与时间）、`document_name`（文件原名）、`has_file`、`owner_id`、`origin`、`origin_label`、`conditions`、`partial_options`、`status`、`status_label`。查找优先级为型号、部件、产品名称、平台。较近的初稿会遮住较远的终稿，旧版本不会代替当前初稿。部件文件隐含所属部件的组合条件。

型号的 `shelf.other_documents` 返回未对应资料要求的其他文件，范围为型号、组成部件、产品名称和平台，不包含其他型号或认证记录上的文件。每项含 `entity`（文件身份）、`document`（版本）、`owner`（所属节点身份）、`origin` 和 `conditions`。复用清单的同一业务时点版本选择，默认每个文件只返回当前版本；不同文件不互相遮盖。部件文件附加所属部件条件，按组合读取时只保留满足条件的文件。原件或云盘链接、客户版本号、文件类别与解析状态均来自版本记录。

型号页的操作历史复用 `GET /entities/:id/history`，每页 20 条。开启货架的型号还包含日志前后值中明确归属该型号的认证和文件版本写入，保留具名 agent、网页、导入及变更集来源。部件和上层节点的独立写入不混入本型号历史；普通库沿用既有范围。

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

## 上层节点与部件详情

产品线、平台、产品名称和部件的 `shelf.documents` 返回明确归属本节点的文件，包含有资料要求及没有资料要求的文件。每项含 `entity`、`document` 及可选的 `requirement` 身份与属性，使用与型号清单相同的业务时间与文件内顺序号选择有效版本。不会混入子型号、其他部件或认证记录上的文件。

产品名称的认证包含自身与平台认证，分别标为 `self` 和 `ancestor`；部件仅返回归属自身的认证。认证的条件和电芯限制保留，不因查看上层节点而推断适用组合。

部件没有自身应交资料清单，也不推断阶段或可售状态；它的文件仍通过 `shelf.documents` 展示，并按现有优先级与条件向使用它的型号共用。同层重叠候选继续标为 `ambiguous`。

网页通过 query 的 `ancestor_id` 或 `part_id` 分页读取全部相关型号，各页携带节点详情返回的 `as_of`。上层节点的矩阵逐项保留选配状态与条件，单元格链接到型号的对应资料要求。顶部缺口项数按不同资料要求去重，受影响型号数沿用 `models_with_gaps`。阶段仅在产品名称提供编辑，修改和清空均确认单操作变更集，保留其他属性和修订检查。

认证列表与节点的 `shelf.certifications` 同时返回 `certification.region`（区域键）及 `region_name`（当前配置显示名）；`shelf_get` 的认证事实也带 `region_name`。型号、部件和上层节点页面显示名称。没有客户生产数据，本次不迁移旧地区；非配置键的旧认证地区仍按原文显示，不报错。修改旧记录的地区更正规则见[认证变更集](product-shelf-changesets.md#认证记录)。
