# 接收故障诊断：实施与验收记录

日期：2026-10-18。实现与复审已完成，保留在隔离分支，未合并或部署。

- 分支：`codex/agent-operations-diagnostics`
- 基准：`5e801f20ab40b8fb2e14209243fef36ff86eb9a3`
- worktree：`/Users/dikui/.codex/worktrees/agent-operations-diagnostics/Reliquary`
- 已确认参数：保留 7 天，包与临时捕获合计 512 MiB；过期优先，再淘汰最旧材料。
- 未执行推送、合并、部署或真实资料拒绝。没有新增依赖或数据库表。

## 交付内容

| 模块 | 改动 |
| --- | --- |
| `internal/diagnostics` | 独立磁盘清单、原始条目/图片/模型调用捕获、校验、分页查询、过期与容量淘汰；存储失败显式降级 |
| 收录服务 | HTTP/MCP 逐项技术故障保留材料；拒绝和成功清临时副本；补交 `recovery_failure_id` 后关联新请求/资料 |
| 审核及删除 | 最终拒绝、丢弃、普通和批量删除前移除关联故障材料；清理失败保留资料以便重试；不删除其他资料的业务图片 |
| HTTP / MCP | 新增列表、详情、材料读取三组接口；文本按 Unicode 分段，图片经认证读取；权限和过期状态一致 |
| 日志 | 禁止相关访问日志和共享模型调试文件复制正文；SQL 日志省略绑定参数；运行事件只记录标识和类别 |
| 前端 | 四类导航；待处理仍为待审、缺图；新增接收故障列表、筛选、详情与只读材料查看 |
| 配置与部署 | YAML / 环境变量配置；独立运行目录挂载；安装生成诊断目录；恢复环境不沿用旧主机诊断路径 |
| 文档 | 更新外部工具、收录、日志和本机部署说明，以及界面设计与交互契约 |

原始快照保持不变。不会自动重放；Agent 读取后通过已有业务接口修正提交。提交后清理异常或连接结果不确定时标为 `unknown`，不得宣称已回滚或已修复完成。

## 已通过的定向验证

1. 独立 PostgreSQL 临时库：混合批次包含成功、拒绝和技术故障，仅故障条目打包；业务回滚后原文及内联图片仍可读取。读取快照后修正重提、关联故障、进入待审，再确认拒绝，材料不可再读。
2. 真实 MCP / HTTP：上传图片后以 `asset_id` 收录，注入队列故障并回滚；删除测试库原上传记录和文件后，故障包图片仍逐字节一致。验证三个 MCP 工具、HTTP 文本和 PNG，以及 401、404、跨空间拒绝。
3. 本机存储：7 天边界、Unicode 分段、哈希与实际字节、最旧淘汰、部分捕获、写入失败、损坏清单不阻断清理、不完整二进制上传读取。
4. 隐私：真实访问日志不复制拒绝输入或诊断材料；资料及收录模型调用不进入共享调试文件；事件不含原文或测试凭据。
5. 收录类型/解码契约、附件、审核与删除的相关现有测试通过。全部 Go 包编译通过。
6. 前端相关 75 项测试、类型检查、生产构建通过；最终页面调整后两个组件场景和类型检查再通过。Chrome 使用模拟 API 验证列表、详情、材料、错误、空状态和 390px 布局，无页面异常。真实协议验证由第 2 项覆盖。
7. 安装脚本 3 项、维护脚本 76 项现有测试通过。`git diff --check` 通过。

复验命令（PostgreSQL 使用开发文档约定的独立测试服务）：

```sh
go test ./... -run '^$'
go test ./internal/diagnostics ./internal/config ./internal/container ./internal/handler ./internal/mcpserver ./internal/middleware ./internal/logger
go test ./internal/types -run 'Test.*Ingest|Test.*Decode' -count=1
# 先按 docs/development.md 设置 RELIQUARY_TEST_PG_DSN
go test ./internal/application/service ./internal/router -run 'TestIngestAuditFailureRollsBackCollectionAndReason|TestIngestAsset|TestMCPAndHTTPReadIngestFailureMaterials|TestMCPInitializeListAndCallAllTools|TestDeleteEntryListIntakeAndResources' -count=1
python3 scripts/test-install-local.py
python3 scripts/test-reliquary-admin.py
```

## 本轮审查问题修复与复验

继续使用原 worktree 和分支。修改约 17 个必要文件，含两处契约说明和本记录；未新增依赖、业务表或清理任务系统。

| 审查问题 | 修复与验证 |
| --- | --- |
| 模型调用原文绕过故障包的保留和容量限制 | 模型记账延至接收结束；失败只保存调用统计，原文只在故障包内。真实模型 HTTP 夹具 + PostgreSQL 验证失败原文不存在于独立账本，成功调用仍可查看详情 |
| 过期查询投影覆盖清单，丢失材料标识 | 存储清单与 API 投影分开。关联、拒绝使用原始清单；清理移除包内除清单外的全部文件，覆盖未列出的中断写入文件 |
| 连续修复失败留下前序故障材料 | 新故障记录 `recovery_failure_id`；拒绝清理同空间内关联恢复链。现有事务回归扩为失败 → 修复再失败 → 成功待审 → 拒绝，逐个确认目录只剩摘要 |
| 业务删除后清理失败无法重试 | 单个审核/删除、批量删除与丢弃统一在业务删除前清理。故障目录设为只读时，拒绝返回错误、资料仍在；恢复权限后重试成功 |
| 包写入失败但已有运行事件不可查询 | 列表和详情合并有界事件日志及滚动归档；包缺失时返回 `unavailable` 无内容摘要，空间隔离保持有效 |
| 拒绝输入经解析错误进入访问日志 | 私密路径只记录结构化错误码。真实 `DecodeIngestBatch` 未知字段反例验证原文不进入日志 |
| HTTP 重复读取材料 | 单次 `ReadText` 返回文本或图片字节；字节不参与 JSON 序列化，MCP 图片仍返回认证 URL。真实 HTTP/MCP 现有契约回归通过 |

独立复审还复现并关闭了进行中捕获的并发缺口：拒绝清理使用既有 active 登记，同时清除相关临时捕获并禁止其再次写入；父故障标记缺失或不可读时仅留摘要。上传捕获与其他资料拒绝共用现有锁，消除新增并发读取的竞态。没有增加持久状态层。

本轮已通过：

- `go test ./... -run '^$'`：全部 Go 包编译。
- `go test ./internal/diagnostics ./internal/config ./internal/container ./internal/handler ./internal/mcpserver ./internal/middleware ./internal/logger ./internal/modelcalls -count=1`。
- `go test -race ./internal/diagnostics -count=1`：含进行中恢复、补交和并发上传回归。
- 独立 PostgreSQL：`TestIngestAuditFailureRollsBackCollectionAndReason`、`TestIngestModelCallBodiesFollowTransactionOutcome`、审核、删除、附件及真实 HTTP/MCP 材料与工具契约。
- 独立只读复审：原文重建、父标记不可读、并发上传三个反例均以 `-race` 反向验证通过；未发现剩余阻断项或需要保留的多余设计。
- `git diff --check`。

本轮未修改前端或部署脚本，未重复运行无关检查。新增回归只覆盖本轮数据保留、拒绝清理和日志隐私风险；没有新增测试框架。

验证日志：`/private/tmp/reliquary-diagnostics-repairs-{compile,unit,pg,integration,concurrency}.log`。此前已存在的失败仍列于下节，不宣称全仓测试全绿。

## 既有问题及验证边界

- 更广的收录回归中，`TestIngestPartialCompletionListPaginationAcceptAndKeyRevocation` 失败：两条夹具标题相同、正文只在图片上不同，既有 SimHash 距离 3 命中去重阈值。使用基准 HEAD 服务代码 overlay 复现相同失败，未修改该业务规则或测试。证据：`/private/tmp/reliquary-head-overlay-test.log`。
- `TestEntryNamingGuard` 报告 `DocContent.vue` 与 `LibraryReviewAction.vue` 的 11 处旧命名；两文件与本分支基准 HEAD 逐字节一致。本次收录契约测试单独通过。
- 前端全仓静态审计发现 21 项历史问题，新增和修改的界面文件无命中。DESIGN 官方 lint 工具未在离线缓存中，未安装额外依赖。报告：`/private/tmp/reliquary-diagnostics-ui-audit.json`。
- 未宣称全仓测试全绿。未验证运行部署；浏览器使用模拟响应，后端协议与事务通过真实 PostgreSQL 测试验证。
- 故障包是尽力保留的短期排障材料，不承诺精确复现外部服务。材料状态、缺失项、到期时间与 `recorded` 字段是恢复能力的判断依据。
