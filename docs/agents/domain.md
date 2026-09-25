# Domain Docs

本仓库采用 single-context，以下规则供 engineering skills 探索代码时使用。

## Before exploring, read these

- 根目录 `CONTEXT.md`：领域术语及模型。
- `docs/adr/`：读取与当前任务相关的架构决策。

如果这些文件不存在，静默继续，不报告缺失，也不提前建议创建。
`/domain-modeling`（经由 `/grill-with-docs` 和
`/improve-codebase-architecture` 调用）会在术语或决策明确后按需创建。

## File structure

- `CONTEXT.md`：仓库统一的领域上下文。
- `docs/adr/NNNN-<decision-slug>.md`：架构决策记录。

## Use the glossary's vocabulary

在 issue 标题、重构提案、假设和测试名称中命名领域概念时，
使用 `CONTEXT.md` 定义的术语，避免使用词汇表明确排除的同义词。

若概念尚未定义，先检查是否符合项目现有用语；
确有缺口时，为 `/domain-modeling` 记录。

## Flag ADR conflicts

如果提案与已有 ADR 矛盾，明确指出对应 ADR、冲突内容和重新讨论的理由，
不要静默覆盖已有决策。
