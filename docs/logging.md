# 日志配置

Reliquary 的日志由 `internal/logger` 统一管理，所有行为通过环境变量驱动，无需改代码。

## 接收故障日志与故障包

技术接收故障在独立诊断目录写入 `events.jsonl`，并尽力保留失败条目的故障包。运行事件只含故障/请求编号、工具标识、时间、库、阶段、错误类别、结果判断和包状态，不包含原文或原始服务商错误。具体错误放在短期包清单中。事件日志每份最大 8 MiB，保留 2 份滚动归档，归档保留期 28 天。

故障包写入失败但事件已保存时，列表和详情仍可通过故障编号查到 `unavailable` 摘要。查询也读取滚动归档；事件本身写入失败时不承诺能查到。摘要不含原文、配置或材料。

包内保存原始条目 JSON（含 Markdown）、实际提交图片、程序版本、已取得的库策略与处理参数，以及可取得且已脱敏的模型调用。材料使用独立标识和 SHA-256 校验。`asset_id` 不能代替图片字节。缺失条件或复制失败会明确标为部分可用；落盘失败通过响应的 `recorded=false` 报告，不覆盖原始业务错误。

默认保留 **7 天**，总容量 **536870912 字节（512 MiB）**。容量包含故障目录中的清单、材料和进行中的临时捕获，不包含独立滚动事件日志。先清理过期材料，再按创建时间淘汰最旧的包；极端数量下也会淘汰最旧摘要。摘要最多保留 28 天，不承诺永久查询。读取到期材料立即返回 410，物理清理每 5 分钟执行一次，不依赖数据库在线。成功、明确拒绝的临时副本立即清理，失败的清理会在后续扫描重试；重启时移除无法继续的临时捕获。

| 配置 | 默认值 | 含义 |
| --- | --- | --- |
| `ingest_diagnostics.root` / `RELIQUARY_DIAGNOSTICS_ROOT` | 系统用户缓存目录下 `reliquary/diagnostics` | 原生运行的独立诊断目录，必须是绝对路径 |
| `ingest_diagnostics.retention_days` / `RELIQUARY_DIAGNOSTICS_RETENTION_DAYS` | `7` | 材料保留天数，1～365 |
| `ingest_diagnostics.max_bytes` / `RELIQUARY_DIAGNOSTICS_MAX_BYTES` | `536870912` | 总容量，至少 1 MiB |
| Compose `RELIQUARY_DIAGNOSTICS_DIR` | `${HOME}/Reliquary/logs/diagnostics` | 宿主机目录，挂载到容器 `/data/diagnostics` |

环境变量优先于 YAML。本机安装脚本使用运行根目录的 `logs/diagnostics`。不要将诊断目录设到 vault、上传目录或源码目录中。故障包不加入业务备份，也不建立数据库资料、索引或图谱。认证头、API Key、模型密钥及整份环境变量不进入故障包。

收录、MCP、资料正文、模型调用详情及诊断材料接口不向共享访问日志复制请求/响应正文。资料关联模型调用只使用可删除的调用内容记录，不写入共享 LLM 调试文件；SQL 日志保留语句并省略绑定参数。确认拒绝、删除关联资料时，清除对应故障材料与配置，只保留不含内容的摘要。既有历史日志或已导出的副本不在本次清理范围。

接收期间的模型调用先暂存。接收成功后保留原有调用详情；接收失败时，独立调用账本只保存统计，原文仅进入受 7 天、512 MiB 限制的故障包。上述私密路径的访问错误日志也只记录错误码，避免解析错误携带拒绝输入。

拒绝或删除前，先清理关联恢复链上的故障材料及进行中的捕获。清理失败则返回错误并保留业务资料，修复存储后可重试。已清理的进行中捕获不会在随后失败时重新保存原文。

查询与修复见[外部工具接入](external-integration.md#接收故障与短期故障包)。

## 环境变量

| 变量 | 是否必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `LOG_LEVEL` | 否 | `debug` | 日志级别，取值 `debug` / `info` / `warn`(`warning`) / `error` / `fatal`，无效值回退到 `debug` |
| `LOG_PATH`  | 否 | 空（仅打到 stdout） | 落盘路径，启用后同时写 stdout 与该文件，文件按 lumberjack 滚动（单文件 50MB / 3 份 / 28 天 / 压缩归档） |
| `LOG_FORMAT` | 否 | 空（沿用内置默认格式） | `json` 输出结构化 JSON；其他值为自定义模板，支持下列占位符 |

> 终端环境下 ANSI 颜色自动启用；非 TTY（容器日志采集、`docker logs` 重定向等）会自动关闭颜色，避免日志聚合/检索异常。

## `LOG_FORMAT` 模板

`LOG_FORMAT` 为空时使用内置默认格式（与历史行为一致）：

```
INFO [2026-05-21 10:20:30.123] [req-abc k1=v1] file.go:42[fn] | message body
```

`LOG_FORMAT=json` 输出结构化 JSON，保留级别、时间、消息与请求编号等字段。

其他非空值启用模板模式，支持如下占位符：

| 占位符 | 含义 |
| --- | --- |
| `%d`       | 时间戳（`2006-01-02 15:04:05.000`） |
| `%level`   | 日志级别（`DEBUG` / `INFO` / `WARNING` / `ERROR` / `FATAL`），开启颜色时仅此占位符被染色 |
| `%thread`  | 当前 goroutine ID。**未引用该占位符时不会调用 `runtime.Stack`，无额外开销** |
| `%logger`  | caller 信息（`file.go:line[func]`），过长时从末尾截取后 50 字符 |
| `%traceId` | 请求 ID（即上下文中的 `request_id`） |
| `%msg`     | 消息正文 + 剩余结构化字段（`key=value`，按 key 升序拼接） |

示例：

```bash
export LOG_FORMAT='[%d] %level %thread %logger %traceId | %msg'
```

输出（终端开启颜色时仅 `%level` 段着色）：

```
[2026-05-21 10:20:30.123] INFO 17 service.go:88[Handle] req-abc | hello extra=ok
```

### 实现细节与注意事项

- 占位符替换使用 `strings.NewReplacer` 单趟扫描，前一个占位符的值即使包含其它占位符字面串（如 `request_id=%msg`）也不会被二次替换。
- 级别染色在 `%level` 替换阶段直接注入 ANSI 颜色，**不会**误染消息正文中字面出现的 `INFO` / `ERROR` 等字符串。
- 结构化字段（`logger.WithField` / `WithFields`）会被拼接到 `%msg` 之后。需要独立字段检索时，使用 `LOG_FORMAT=json`。
- `LOG_LEVEL` 与 `LOG_PATH` 的设置会在 `main` 加载 `.env` 后由 `logger.ConfigureFromEnv()` 重新生效。
