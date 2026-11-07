# 产品地图导入

导入只创建待确认变更集。确认、修订检查、逐项修正、暂缓和单事务写入沿用[货架变更集](product-shelf-changesets.md)。上传的工作簿不成为资料文件，也不保留客户原件。

## 导入方案

`GET /libraries/:id/shelf/import-plans` 返回当前库的方案，按 `key` 排序。每个文件种类用一个稳定的方案键。当前支持 `file_kind=product_map`。

方案也通过变更集保存，没有直接修改货架的旁路：

```json
{
  "source": {"type": "web"},
  "operations": [{
    "id": "map_plan",
    "type": "save_import_plan",
    "import_plan": {
      "key": "product_map",
      "name": "产品地图",
      "file_kind": "product_map",
      "revision": 0,
      "sheets": [{
        "worksheet": "Map",
        "product_line_id": "existing-line-id",
        "header_rows": 2,
        "columns": [
          {"column": "A", "field": "platform"},
          {"column": "B", "field": "product"},
          {"column": "C", "field": "model"},
          {"column": "D", "field": "sale_status", "key": "configured-region"},
          {"column": "E", "field": "parameter", "key": "configured-energy"},
          {"column": "F", "field": "description"}
        ]
      }],
      "model_separators": [";", "\n"],
      "missing_values": ["待定", "型号待定", "TBD"],
      "status_values": {"GA (Demo)": {"status": "GA", "notes": "Demo"}},
      "fill_meanings": {"FFFFFF00": "uncertain"}
    }
  }]
}
```

产品线、区域及参数定义先在本库确认。每张工作表分别指定表头行数；列用 Excel 字母定位，不依赖表头文案。平台、产品名称和型号列必填。参数仅接受归属该产品线的活动文本、数字、布尔、日期或单选字段。未知工作表必须显式映射或设 `ignore: true`。未映射的列忽略。

首次保存的修订号为 1；更新须提交当前 `revision`。读取结果的方案内容在 `definition` 内，写入时使用上例的平铺字段。方案的工作表、拆分规则、占位值、状态写法与颜色含义都由提交方确认，不包含客户默认配置。模型建议由后续能力生成。

`model_separators` 按列出的顺序拆分型号，不修剪或改写拆分后的名称。`missing_values` 明确列出不能视为型号等身份字段的占位写法。数值参数支持数字及 `9~54`、`9～54` 形式的闭区间；布尔接受 `true/false/1/0`；日期接受 `YYYY-MM-DD`。

`status_values` 将确切原文映射为固定状态及备注，仍保存原始状态文字。空白写法不能配置默认状态，须逐行决定。填充色使用大写 ARGB，如 `FFFFFF00`；索引色使用 `indexed:13`，主题色使用 `theme:4:0.5`，无 tint 时末尾为空。颜色含义可为 `ignore`、`uncertain`、`planned_eom` 或固定状态。固定状态颜色只核对区域文字，不替空白区域填写状态。身份或描述列的 `planned_eom` 作用于该行区域，区域列的标记只作用于该区域。未知颜色、无法确定含义的颜色和文字矛盾都成为待确认冲突。

## 上传与确认

`POST /libraries/:id/shelf/imports` 使用 multipart：

| 字段 | 含义 |
| --- | --- |
| `file` | `.xlsx` 工作簿 |
| `plan_key` | 已确认方案的键 |
| `file_date` | 可选 RFC3339 生效时间；取不到可靠文件日期时必填 |

文件名中恰好有一个有效日期时，识别 `YYYYMMDD` 或用 `-`、`.`、`_`、`年/月/日` 分隔的两位月日，按 UTC 零点生效。没有日期、日期异常或多个日期时要求填写，不使用上传时间或 Office 的编辑时间。显式日期优先，零时间和未来时间拒绝。

返回 `201` 与普通待确认变更集。同一库、文件内容、方案键及修订、生效时间确定一个批次；重复上传返回该批次及已保存的处理进度。文件重命名不改变内容身份；返回来源保留首次上传的文件名。上传不自动确认。

导入沿用名称、别名和代码的确定匹配，不做模糊身份推断。合并单元格只展开明确的合并范围，不向空白行填充前一行；两行或多行表头按方案跳过。同一型号重复行、跨平台同名产品、分组标题、空区域、未知状态、公式和无法解析的参数交给提交方处理。必填缺失行不生成层级或型号；区域空白可修正相应属性提议为无记录，或明确填写状态。

已有属性以文件日期的快照为基础，只提议映射字段；未映射字段及后续快照继续沿用属性时间线规则。手工值与导入提议不同时，变更集展示原值并等待明确决定。不同类别同名仍须确认。

上一已确认文件的活动型号在新文件中消失时，生成 `missing_entity`，由提交方选择保留或退市，不删除实体。同一导入批次的已确认部分和后来确认的暂缓部分合并比较。首次导入不把库中其他型号视为消失。

来源包含 `import_batch_id`、`import_plan_key`、`plan_revision`、`file_name`、`file_sha256`、`file_date`；每项的 `import_location` 保存工作表和单元格，确认日志也带相同位置。方案、批次、决定和来源随维护副本导出恢复。迁移 49 新增方案表，存在方案或导入批次时拒绝开发降级。

## 文件边界

单文件及 ZIP 解压后的总量最多 32 MiB；最多 2,000 个 ZIP 项，每张表最多 10,000 行、100,000 单元格和 10,000 个合并范围。批次沿用 500 操作上限，超过时拒绝并要求拆分工作簿。原件只暂存于请求解析阶段，结束后清理。

只读取数据型 `.xlsx`，不执行公式或访问外部引用。自定义数字显示格式、条件格式等不能确定含义的内容须先提供明确值；不通过推测补全型号或自由文本参数。测试在代码中构造脱敏片段，客户原件不进入仓库。
