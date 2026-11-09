## Agent skills

### Issue tracker

Issues 和 specs 使用本地 Markdown，存放于 `.scratch/<feature-slug>/`。详见 `docs/agents/issue-tracker.md`。

### Triage labels

使用五个默认 triage 状态，记录在 issue 文件的 `Status:` 行。详见 `docs/agents/triage-labels.md`。

### Domain docs

采用 single-context：根目录 `CONTEXT.md` + `docs/adr/`。涉及业务或架构设计时，参阅 `docs/agents/domain.md` 中的资料入口与按需读取规则。

### Document layout

文档放置约定见 `docs/agents/layout.md`。

### Local deployment

更新本机部署前，读取源码根目录的 `.deploy.env`，以 `RELIQUARY_DEPLOY_DIR` 定位运行目录；该文件不提交。不要将源码目录当成运行目录。目录结构与维护步骤见 `docs/local-deployment.md`。

部署备份约定：

- Agent 每次发起备份前，必须说明备份原因与范围，并等待人工明确同意本次备份；部署、升级、迁移或批量处理请求不视为备份授权。
- 部署与备份解耦。代码更新、全量构建和数据库升级默认不备份；必要的数据库迁移仍须执行，不能以跳过迁移代替跳过备份。
- 询问用户是否部署时，同时说明本次数据变更范围、不备份是否影响部署执行、具体的数据损失风险及失败后的恢复能力。判断应依据本次代码与迁移内容；不把“不备份”直接等同于“部署会出错”，也不承诺没有数据风险。
- 清库、删除数据卷、覆盖存储或大规模不可逆改写前，先明确是否需要保留操作前的恢复能力；需要时单独备份并确认校验通过。普通业务读写和数据库迁移本身不触发强制备份。
- 定时备份按独立月度计划执行，不与每次部署绑定；跳过部署前备份不应关闭或修改定时备份和恢复演练计划。无本次备份时，不得声称能恢复到本次操作前的最新状态。

### Testing

开发中只运行受影响的测试，按 `/local-test` 做：后端 `make test-pkg PKG="<包> ..." [RUN='<用例正则>']`，前端 `cd frontend && npm test -- <测试文件>`。需要 PostgreSQL 或 Redis 的用例先设置 `docs/development.md` 中的 `RELIQUARY_TEST_PG_DSN`、`RELIQUARY_TEST_REDIS_ADDR`（本机容器 `reliquary-test-pg`、`reliquary-test-redis`）。开发中不加 `-count=1`，改过的包不会命中缓存。全量测试（`make test-pg` 连本机测试库，约 6 分钟，加前端检查）由 `.githooks/pre-push` 在版本号（`VERSION`）前两位变化时运行。

### Commit rules

禁止 `--no-verify`、`git add -A` / `git add .` 和强推。

## 偏好

- 项目只提供接口，不负责信息采集和跟踪；外部工具送完整文件，送不了才送链接，并通知人工（2026-11-08，来源：Reliquary 接入讨论）。
- 入库质量要高；导入内容统一转成 Markdown，不保留原件（2026-11-08，来源：Reliquary 数据类型讨论）。
- 实体是预设的；新实体先进待确认区，人工通过后才能使用（2026-11-08，来源：Reliquary 实体讨论）。
- 只检测重复，不检测冲突（2026-11-08，来源：Reliquary 资料自检讨论）。
- 维护和恢复要求严格，不能丢信息；迁移和新环境重建集成到一个命令里；周备份，只保留 4 周（2026-11-08，来源：Reliquary 维护讨论）。
- 不做多设备同步；暂时只在内网访问，不考虑手机端（2026-11-08，来源：Reliquary 部署讨论）。
