# 0027 破坏性操作只在 MCP 层确认

Status: accepted
日期：2026-11-14

## 决策

MCP 工具 `delete_document` 和 `discard_pending` 须带 `confirmed=true` 才执行；未确认时只读取目标说明风险，返回 `isError` 结果，含错误码 `confirmation_required`、风险说明和“先取得用户明确同意再重试，不得自行补上 confirmed”的提示，不做任何修改。确认只在 MCP 层检查：HTTP 删除与丢弃接口、服务层行为不变。`ingest` 覆盖旧文和 `update_entry_content` 不加确认，只靠工具标注和使用规则约束。

## 取舍与验证

不是所有 MCP 客户端都会按破坏性标注拦截调用，agent 一次误调就会删掉资料，所以服务端多加一次停顿。HTTP 接口由网页前端使用，已有人工操作确认，加参数会破坏现有调用方。`ingest` 覆盖与改正文是采集程序的日常流程，加确认同样会破坏调用方。这道关不能阻止 agent 自行补上 `confirmed=true`，规则文件与常见错误表明确禁止这样做。

测试验证两个工具不带 `confirmed` 时返回 `confirmation_required`，资料和待审仍在；带上 `confirmed=true` 后才执行。
