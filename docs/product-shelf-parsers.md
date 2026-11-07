# 货架资料解析

开启货架的库通过 `configure_shelf` 变更集操作保存 `library.shelf_config`。配置和重解析都先提议、再确认。网页单项设置也沿用单操作变更集。现有库没有解析器配置时只保存原件。

## 配置

```json
{
  "parsers": [
    {
      "key": "external",
      "name": "外部资料解析服务",
      "type": "http",
      "url": "http://localhost:7000/parse",
      "authorization": "Bearer 服务凭据",
      "timeout_seconds": 120,
      "concurrency": 2
    },
    {
      "key": "visual",
      "name": "视觉读取",
      "type": "vision",
      "model_id": "vision-model-id",
      "render_dpi": 144,
      "prompt": "逐页读取原文，保留表格、编号和单位。"
    },
    {"key": "skip", "name": "不解析", "type": "none"}
  ],
  "default_parser": "external",
  "parser_rules": [
    {"extension": ".zip", "parser_key": "skip"},
    {"requirement_id": "requirement-id", "parser_key": "visual"},
    {"file_category": "提交方定义的文件类别", "parser_key": "external"}
  ]
}
```

`key` 是库内身份，`name` 原样保存。配置解析器后必须明确提供默认解析器。规则按数组顺序查找，首条命中即采用；同一规则中填写的扩展名、资料要求和文件类别必须同时匹配。扩展名含点，不区分大小写。资料要求用本库 ID，类别原样比较。类别优先使用版本输入中的 `file_category`，未填时使用资料要求当前的文件类别；选择结果与类别保存在版本中。未命中规则使用默认解析器。

HTTP 超时为 1 至 3600 秒，并发为 1 至 100。`authorization` 是完整 Authorization 头，也可省略；地址是维护者明确配置的可信服务，可位于本机或局域网。请求不跟随重定向。超时包含等待该解析器的并发空位及服务调用；并发限制按库和解析器键在 PostgreSQL 中协调。

视觉解析必须配置可处理图片的模型、72 至 600 的渲染 DPI 和提示词。模型仍使用现有凭据、并发治理和调用记录。PDF 运行环境需要 Poppler 的 `pdfinfo` 和 `pdftoppm`，应用镜像已包含。每页按配置的精度渲染，超过现有模型图片边长或像素限制时降低渲染精度；普通静态图片直接经过同一图片预处理。逐页转为 Markdown，不实现 Office 转换。渲染文件只在临时目录存在，任务结束清除。

## HTTP 服务协议

向配置地址发送 `POST multipart/form-data`，字段 `file` 包含完整原件与原始文件名。成功返回 2xx 和以下 JSON：

```json
{
  "pages": [
    {"page": 1, "markdown": "# 原文标题\n\n第一页正文"},
    {"page": 2, "markdown": "第二页正文"}
  ]
}
```

页码必须为递增正整数，每页 Markdown 非空。响应最多 20 MiB，最终正文同时遵守现有 500 万 Unicode 码点限制。Reliquary 为每页统一添加 `<!-- page: 1 -->` 形式的页码标记，不推断页码或缺失内容。具体解析服务的选型及部署由维护者决定，使用其他协议的服务需要在服务侧适配。

## 队列与文本

确认带本机原件且没有手工文本的版本时，在同一事务中选择解析器并写入现有持久任务队列。仅有云盘链接的版本不下载、不解析。配置为 `none` 时返回 `not_parsed`，不创建文本或解析任务。

解析状态为 `pending`、`parsing`、`retrying`、`failed`、`completed` 或 `not_parsed`。失败采用现有队列退避，最多重试 3 次。最终失败与任务归档同事务记录，保留原件和错误说明，不发布部分文本。解析成功后，Markdown、该版本的 `entry_id`、到文件与所属节点的显式资料关联，以及普通正文索引任务同事务保存。`completed` 表示已有文本，不代表后续摘要、向量等任务均已完成。

版本还返回选中的 `parser_key`、请求代数 `parse_attempt`、失败说明 `parse_error` 和文件类别。每次执行或失败重试读取所选解析器的最新配置，修改配置不会自动重解析历史版本。删除仍被任务使用的解析器会使该任务明确失败，不回退到其他解析器。

## 重解析

通过已有变更集接口提交：

```json
{
  "source": {"type": "web"},
  "confirmed": true,
  "operations": [{
    "id": "reparse",
    "type": "reparse_document",
    "entity_id": "document-file-id",
    "revision": 3,
    "reparse": {
      "version_id": "document-version-id",
      "parser_key": "visual"
    }
  }]
}
```

`revision` 是资料文件的修订号。必须明确指定本库解析器和带本机原件的版本，历史版本也可以重解析。确认后增加文件修订号及解析请求代数，记录带来源的 `reparse_document` 操作日志。版本 ID、顺序号、客户版本号、当前版本标记和原件均不改变。旧任务的迟到成功或失败不能覆盖新请求。

已有解析文本会先撤除正文索引和派生信息，再隐藏旧文本；改成 `none` 时版本只保留原件。再次解析成功使用同一文本 ID。若旧文本正在后台处理，返回修订冲突，须等该任务结束再提交。调用方另行收录并手工指定的独立文本不由解析器覆盖，返回 `uncertain` 交给提交方修正。

迁移 48 增加解析状态字段，旧版本获得空解析器和零请求代数。存在解析配置、请求、文件类别或重解析变更集等新数据时拒绝降级。完整数据库备份保留配置；无凭据导出沿用现有规则移除 Authorization 并记录待补凭据，恢复后需重新填写认证。
