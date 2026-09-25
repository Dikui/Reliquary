# Triage Labels

五个 canonical triage roles 使用默认字符串。
在本地 Markdown issue 中，将对应字符串写入 `Status:` 行。

| Canonical role | 本仓库状态 | 含义 |
| --- | --- | --- |
| `needs-triage` | `needs-triage` | 等待维护者评估 |
| `needs-info` | `needs-info` | 等待报告者补充信息 |
| `ready-for-agent` | `ready-for-agent` | 需求明确，可交由 Agent 实现 |
| `ready-for-human` | `ready-for-human` | 需要人工实现 |
| `wontfix` | `wontfix` | 不予处理 |

当 skill 提及某个 triage role 时，使用表中对应的状态字符串。
之后可直接修改此表以调整命名。
