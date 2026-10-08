# MCP 工具人工审核（危险调用）

对应需求：智能体调用 MCP 工具前可中断，待人工确认后再执行（GitHub #1173）。

## 行为说明

1. 在 **设置 → MCP** 中连接测试成功后，在工具列表上打开 **「需人工审核」** 开关，即可为该工具打标。
2. Agent 运行时若即将调用已打标的工具，会推送 `tool_approval_required` 事件，对话界面展示审批卡片（可编辑 JSON 参数）。
3. 用户 **通过** 或 **拒绝** 后，后端恢复执行；拒绝时工具返回错误信息给模型，不会调用远端 MCP。
4. 若超时未处理（默认 10 分钟，可通过配置 `agent.tool_approval_timeout_seconds` 调整），视为拒绝。

## 配置示例

任选一种：

**1. config.yaml**

```yaml
agent:
  tool_approval_timeout_seconds: 600  # 可选，默认 600（秒）
```

**2. 环境变量**（优先级高于 yaml）

```bash
# 支持纯秒数或 Go duration（30s / 5m / 1h）
RELIQUARY_AGENT_TOOL_APPROVAL_TIMEOUT=600
```

## API

- `GET /api/v1/mcp-services/:id/tool-approvals` — 列出已保存的审核配置  
- `PUT /api/v1/mcp-services/:id/tool-approvals/:tool_name` — 设置某工具是否需审核（`{"require_approval": true}`）  
- `POST /api/v1/agent/tool-approvals/:pending_id` — 在审批卡片中提交结果  
  - body: `{"decision":"approve"|"reject","modified_args":{...}可选,"reason":"..."可选}`

## 部署与限制

- **审批等待状态保存在进程内存** 中：`pending_id` 仅对当前实例有效；进程重启后进行中的等待会失败（表现为拒绝/取消）。
- **单后端进程**：审批请求和决定保存在进程内存中，超时和取消仍会结束等待；服务重启后未完成审批需重新发起。
- **审批等待不会被工具默认 60s 超时取消**：审批阶段使用 round 级别的 ctx（不带 `defaultToolExecTimeout`），仅受 `agent.tool_approval_timeout_seconds` 与请求级取消控制。
- 安全边界：审核通过后的参数仍由当前登录空间提交；请仅在可信环境下授予「通过」权限。
