# 调研精简资料、来源追溯与后续补充的产品做法

Parent: [Reliquary 资料库与模板规则决策地图](../map.md)
Labels: wayfinder:research
Type: research
Participation: AFK
Status: resolved
Assignee: Codex research agent (/root/library_patterns)
Blocked by: none

## Question

同类产品怎样支持简短参考资料、个人笔记与订阅内容共存，以及同一来源的后续补充？哪些做法可以供现有 Reliquary 的规则设计参考？

只选少量有代表性的产品，查官方文档或源码。重点区分：保存的正文与摘要、用户笔记与原始来源、来源链接与时间信息、重复 URL 和后续补充、检索时的实用信息。

报告须区分官方事实、未找到官方说明的事项、对 Reliquary 的推论。不可用外部产品现状替用户决定接收门槛、库数量或模板数量；不评选替代程序。

产物：`docs/research/library-template-patterns-2026-10-21.md`。调研在现有隔离工作树的 `research/library-template-patterns` 分支进行；只保存研究文档，不推送。

## Answer

已完成 Reader、Raindrop.io 和 Capacities 的官方文档调研，未进行账号内实测。详见[精简资料、来源与后续补充：三种产品做法](../../../docs/research/library-template-patterns-2026-10-21.md)。

可供决策的事实：订阅接收与长期收藏可以分开；来源内容、摘要、个人笔记可以独立保存；同 URL 去重不等同于原文更新。产品在刷新原文、重新提取和增量补充方面各有边界，官方未说明之处已标为未知。

对 Reliquary 的建议均保留为待确认推论。全文保留、最低门槛、库数量和模板数量尚未由用户决定。

## Comments

- 2026-10-21：用户允许调研其他产品。已领取，待研究代理完成；不得开始其他 HITL 问题的代答。
- 2026-10-21：研究完成并关闭。报告含比较表与 18 个官方来源。临时分支 `research/library-template-patterns`，文档提交 `77ce36dbc0892b0bd1c66b6b2b5fbab6570e9db8`；未推送。已将同一报告同步到主工作目录供后续讨论，未合并或修改程序代码。
