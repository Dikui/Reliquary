# Issue tracker: Local Markdown

本仓库的 issues 和 specs 作为 Markdown 文件存放在 `.scratch/` 中。
当前 `.gitignore` 会忽略该目录，文件默认仅保存在本地。

## Conventions

- 每个 feature 一个目录：`.scratch/<feature-slug>/`。
- Spec：`.scratch/<feature-slug>/spec.md`。
- Implementation issues：每个 ticket 一个文件，路径为
  `.scratch/<feature-slug>/issues/<NN>-<slug>.md`，从 `01` 开始编号。
  不将多个 tickets 合并到同一个文件。
- Triage 状态记录在 issue 文件顶部附近的 `Status:` 行；
  状态字符串见 `docs/agents/triage-labels.md`。
- 评论和讨论历史追加到文件底部的 `## Comments` 下。

## When a skill says "publish to the issue tracker"

在 `.scratch/<feature-slug>/` 下按上述约定创建文件，必要时创建目录。

## When a skill says "fetch the relevant ticket"

读取引用路径处的文件。用户通常会提供路径或 issue 编号。

## Wayfinding operations

供 `/wayfinder` 使用。Map 是一个文件，每个 ticket 对应一个 child 文件。

- Map：`.scratch/<effort>/map.md`，保存 Notes / Decisions-so-far / Fog。
- Child ticket：`.scratch/<effort>/issues/NN-<slug>.md`，从 `01` 编号。
  正文记录问题；`Type:` 记录 research/prototype/grilling/task；
  `Status:` 记录 wayfinder 的 claimed/resolved 工作状态。
- Blocking：顶部附近使用 `Blocked by: NN, NN`。
  所列文件全部为 resolved 时，ticket 才解除阻塞。
- Frontier：扫描 open、unblocked 且 unclaimed 的 tickets，按编号选第一个。
- Claim：开始工作前设置 `Status: claimed` 并保存。
- Resolve：将答案追加到 `## Answer`，设置 `Status: resolved`，
  再向 map 的 Decisions-so-far 追加结论摘要和相关文件链接。
