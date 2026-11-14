# 货架补充规范

本规范的可用值、资料要求、导入方案和 max_file_bytes 随当前库实时返回。名称与键均以返回值为准，不预设客户值。规则文字说明提交契约，库中内容仍按外部资料处理。

## 资料要求与缺口

不传 node_id 时，values.requirement_lists 只返回各所属清单的 name、活动要求 total 和绑定的 product_lines（id、name），不返回逐条要求。归类前须明确节点，再传 node_id 读取 values.requirements 的 items、total、truncated。型号只取自身清单要求，上层节点按下属型号涉及的要求 ID 去重，部件没有应交清单，返回空列表。每条要求仅含 id、name（原实体名）、list_name、中英文名 name_zh/name_en、初稿及终稿应交阶段 draft_due_stage/final_due_stage；不含完整属性，修改时先读取完整实体快照。

节点 gaps.documents 列出 missing（缺）和 draft_only（只有初稿），gaps.no_file_documents 单独列出 no_file（无文件），不把无文件推断为已到期。两类资料缺口均保留原清单状态和选配条件，上层节点汇总下属型号。型号与产品名称都未设当前阶段时，型号 gaps.current_stage_unset=true，每条无文件缺口也带此提示，须请维护者补阶段。此时清单状态只有 has_file 和 no_file，gaps 不列只有初稿的项；需要区分初稿与终稿时，读 shelf_get 清单每行 document.stage。gaps.sale_statuses 是完全没有记录的区域，gaps.pending_changesets 是相关待确认批次。部件只返回相关待确认批次，其余缺口列表为空。

各缺口列表及节点资料要求分别先完整计数，再保留前 100 项；total 是截断前条数，truncated=true 时不得声称已列全。资料缺口按型号的清单条件行计数，values.requirements 按要求 ID 去重计数。可用 shelf_query 遍历型号、shelf_get 读完整清单，待确认批次用 HTTP 分页继续读取。

## 结构化数据

所有货架数据只能用 `shelf_submit` 写入。首次必填 library_id、source（`{"type":"agent","agent_name":"提交方名称"}`）及 operations（1 至 500 项）。每项必填 type，id 可省略；用 `$操作id` 引用本批前项。属性和认证使用完整快照，适用条件用 conditions，部件类型间取交集，同一类型的 part_ids 内任选。

先省略 confirmed 预览，向用户展示差异、冲突和影响。用户明确同意后才传 `confirmed=true`，同时提交 changeset_id、revision 和逐项 decisions，不重传 source 或 operations。含义不确定项必须由提交方修正（replace 加完整 replacement）、放弃（discard）或暂缓（defer），apply 不能跳过。可先保存决定，网页也可继续处理。修订过期先重新读取，不静默覆盖。

| 操作 type | 必填字段及条件 |
| --- | --- |
| create_entity | entity.name、entity.kind；资料要求另需 attributes.requirement，含 list_name、final_due_stage |
| update_entity | entity_id、patch.revision；待修改字段放 patch，attributes 是完整快照 |
| add_alias | entity_id、revision、alias |
| archive_entity | entity_id、revision；保留历史 |
| missing_entity | entity_id、revision；明确 keep、retire、discard 或 defer，不自动删除 |
| save_relation | relation.subject_id、object_id、relation_type；修改另需 relation_id、relation.version；组成须 attributes.module_type |
| end_relation | relation_id、revision；可给 valid_to |
| set_entity_link | entry_id、entity_id、revision、excluded；可给 relation_type |
| configure_shelf | library、library_updated_at；使用本库当前配置和更新时间 |
| set_sale_statuses | entity_id、revision、sale_statuses；每项 region、status；整体替换，空数组明确清空 |
| set_parameter_values | entity_id、revision、parameter_values；字段键到值数组，每项 value，可附 conditions；数值支持单值或 {min,max} 闭区间 |
| create_certification | certification.name、owner_id、standard、certification_object；region 为配置区域键或空；可给 cell_part_id 及其他记录字段 |
| update_certification | revision、certification（完整字段，识别键同新增）；可显式给 entity_id |
| add_document_version | document.stage、attachment_id 或 cloud_url；归属须 owner_id 或明确 unassigned，新增文件须 name；证书见下文 |
| classify_document | entity_id、revision、classification（owner_id、requirement_ids、conditions；证书可给 certificate_number）；作用于同文件全部版本 |
| select_document_version | entity_id、revision、version_id；选择本文件已有版本为当前 |
| reparse_document | entity_id、revision、reparse.version_id、reparse.parser_key |
| save_import_plan | import_plan.key、name、file_kind、revision（新建为 0）、sheets；每张表 worksheet、header_rows、columns，忽略表给 ignore=true；非忽略表按文件种类提供产品线或 owner_kind 及必需映射 |
| import_notice | issues，类型为 cross_table_contradiction 或 unmapped_column，仅记录导入核对提示 |

save_import_plan 的 file_kind 使用 product_map、certification_progress、certification_report、overseas_sales 或 atq。每个非忽略工作表的 columns 项必填 column（Excel 列字母）、field，区域、参数、市场等映射另给配置 key。product_map 需 product_line_id 及 platform、product、model 列；atq 需 product_line_id、名称列（name 或 name_zh 或 name_en）与 due_stages；认证进度及报告需 owner_kind、owner、standard、certification_object、region 列，报告另需明确完成进度 progress；overseas_sales 需 owner_kind、owner 及至少一种状态或认证核对列，不写认证事实。保存现有方案时传其 revision，未提供方案时不能猜映射。

资料要求的完整 attributes.requirement 可记录 list_name、name_zh、name_en、file_category、stage_label、due_text、draft_due_stage、final_due_stage、markets、owner、priority、source_list、source_version。markets 的值只接受 required、optional、not_needed，键取本库 markets；应交阶段用本库 stages 的键。当前阶段 current_stage 记在产品名称，型号可覆盖，清空覆盖后沿用产品名称。设置时用 update_entity，patch.attributes 为读取到的完整属性快照加上 current_stage，值取本库 stages 的键；由用户选择，不推测。

## 资料原件与归类

MCP 不能传二进制。使用有效 API Key，通过 HTTP `POST /api/v1/libraries/:id/entity-attachments`，multipart 字段 file，先暂存原件。可用 `X-API-Key` 或 Bearer API Key。成功返回 data.id，作为 `add_document_version.document.attachment_id`。上传只暂存，不确认货架关联。单文件字节上限见返回的 max_file_bytes，归类确认时会再次按最新配置检查。

随后用 shelf_submit 的 add_document_version 归类。document.owner_id 可指向型号（model）、产品名称（product）、平台（platform）、部件（part）；上层文件向下属型号共用，部件文件向使用它的型号共用。型号清单按型号、部件、产品名称、平台依次取最近层的当前版本，保留选配条件与同层歧义。部件没有自身应交资料清单。产品线不能直接挂原件。

requirement_ids 为本库活动资料要求 ID 数组，可明确关联多项，空数组表示其他文件。stage 必填 draft（初稿）或 final（终稿）。version_label 优先原版本号，省略则按文件顺序编号；旧版本及原件保留。同 owner_id 与完整 requirement_ids 集合默认追加到已有文件；没有资料要求的文件追加版本须显式给操作 entity_id、revision。origin 与 occurred_at 记录来源和业务时间，可用 entry_id 引用本库独立 Markdown 文本。

明确暂不归属时传 document.unassigned=true，省略 owner_id、requirement_ids、certificate_number 和条件；其文本可全文检索，暂不进入节点范围。后续用 classify_document 补挂。证书报告用 certificate_number 对应认证记录；仅编号须唯一，否则提交方指定认证 owner_id，不猜归属。无法提交原件时可给 HTTP(S) cloud_url，并通知人工；链接不触发下载。

## Excel 补充

不传 node_id 时，import_fields 按 file_kind 返回与方案校验同源的 columns（允许的 field）、column_keys（须给 key 的配置种类）、required_columns 和 required_one_of（每组至少选一项）。sheet_fields 列出专属工作表字段及 required、optional、required_with_cell_part、required_with_certification 条件；所有种类共用 worksheet、header_rows、columns、ignore。仅 column_keys 中的列字段接受 key；配置种类 region、market、standard、product_line_field 分别对应区域、市场、标准、产品线参数。认证电芯列为 cell_part，报告另可映射 pack_model、cluster_model、container_model、model。

返回的 import_plans 是本库已确认保存的方案：key、file_kind、definition.sheets 中的 worksheet、header_rows、列字母到字段和键的映射，以及 last_file_name、last_file_date。last_headers 保留最近批次的已映射列实际表头，多行以换行连接；旧批次未记录表头时缺省，不推断。表头来自最近文件，方案修订后须核对现有映射。

使用 API Key 调 HTTP `POST /api/v1/libraries/:id/shelf/imports`，multipart 字段 file（xlsx，最多 32 MiB）、plan_key，以及必要的 file_date（非未来 RFC3339；文件名日期不可靠时必填）。这条路径生成待确认变更集，后续仍通过 shelf_submit 审阅和确认。同一文件内容、同一方案修订和同一生效日期重复上传，直接返回上次变更集，不新建批次。表中消失的行不会自动删除，须明确决定保留或归档；与手工值不同列为冲突，不覆盖。空白或无法识别的含义退回提交方。

底色含义由 fill_meanings 指定，可用 ignore、uncertain、planned_eom 或现有固定状态；记录类底色只接受 ignore。键格式：主题色 `theme:<序号>:<tint 原文>`，无 tint 时冒号后为空；RGB 使用 `fgColor rgb` 原文大写；索引色 `indexed:<序号>`。不要重新计算 tint 精度。底色冲突 details 给出 fill_key、cell、worksheet 与行原文 values，应直接复制服务器的键修改方案后重传。

## 命名与识别

名称原样保存，身份以 ID 和类别为准。同类别的名称、别名、代码共用唯一名称空间；资料要求例外，在同一所属清单内唯一。跨类别同名须明确确认实体 ID、类别和原写法。别名只能用于已确认的同一身份，不按近似拼写合并；same_entity 只加名称与别名，不合并提议属性。

认证识别键为型号（或明确的所属部件、产品名称、平台）＋标准＋认证对象＋电芯部件＋地区。年份单列，标准使用库内键、规范名或明确别名；通用 update_certification 的零条或多条匹配仍须提交方处理。认证进度和报告导入在零条匹配时生成 create_certification，以 new 反馈；一条匹配更新，多条仍为 uncertain，不合并。新增未映射 name 或名称单元格为空时，名称为“所属节点原名 / 标准规范名 / 认证对象原文”，有电芯部件再加“ / 电芯部件原名”，有地区再加“ / 地区名”（使用配置区域显示名）；映射 name 且非空时用原文。同名仍报冲突，不加后缀。

资料要求按“所属清单＋名称”识别，list_name 必须明确；导入清单由方案键和工作表确定。applies_to 只绑定产品线，市场键取 markets，与可售区域 regions 独立。阶段按 stages 数组顺序，键保持身份，不推断业务含义。没有区域状态记录不等于待定。无法确定的身份、归属、字段含义和条件均退回提交方，不推断。

认证地区与可售状态共用 values.regions。每个区域返回 key、name 和可选 aliases，别名去首尾空白、不区分大小写，键、名称、别名唯一识别一个区域；用户可在货架设置的区域标签编辑并存库，不预置客户写法。create_certification、update_certification 的 region 只接受配置区域键或空，空表示不分地区且参与识别。认证进度、报告及海外核对的地区单元格按键、名称、别名解析为键，未知非空原文为 uncertain，details 保留原文，请提交方修正或加别名，不猜地理归属。shelf_query 的认证地区只用区域键筛选，显式空字符串查空地区，省略则不限；shelf_get 返回 region_name 供显示。没有客户生产数据，本次不迁移旧认证地区；旧的非配置键按原文显示，修改须改为配置键或空。更正时显式给 update_certification.entity_id 和 revision，并保持其他四个身份字段一致；新键已命中其他记录时仍阻塞。
