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

### Commit rules

禁止 `--no-verify`、`git add -A` / `git add .` 和强推。
