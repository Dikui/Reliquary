# 文档理解后续问题

记录日期：2026-10-17。

这些任务来自 A 方案实施、同六篇资料的项目 API 复测及后续对话。建档不表示问题已经修复，也不表示已批准部署或修改历史数据。

| 编号 | 问题或交付事项 | 优先级 | 状态 |
|---|---|---|---|
| [01](issues/01-entity-name-search-failure.md) | 直接检索实体名显示失败 | P1 | ready-for-agent |
| [02](issues/02-unsupported-entity-descriptions.md) | 简介仍补入无原文依据的身份或动作 | P1 | needs-triage |
| [03](issues/03-historical-entity-cleanup.md) | 历史实体污染、空简介及重复档案尚未处理 | P2 | needs-triage |
| [04](issues/04-rollout-document-understanding.md) | A 方案改动尚未部署 | P2 | needs-triage |
| [05](issues/05-content-classification-review.md) | 两篇分类超出预标注集合，需核对分类口径 | P2 | needs-triage |

建议先定位检索失败并解决简介准确性，再确定部署和历史数据处理顺序。分类项先确认标准，不能只因与预标注不同就认定模型错误。

## 已完成与验收边界

A 方案已接入实体保留标准、短介绍、段落编号证据和 company/organization 跨类型身份候选。另修复摘要围栏 JSON 解析，以及无候选时多余的身份判断调用。

最终同六篇资料首次全部处理入库，34 个按篇计数的实体、23 条关系、核心对象 24/24、129 个有效证据位置；服务层全量测试通过。这些结论仅覆盖本批资料，不能当作全库验收。关系证据是已有能力的修复，不是新增业务需求。

## 证据

- [实现与验证报告](../tasks/document-understanding-20261017/implementation/report.md)
- [真实 API 汇总](../tasks/document-understanding-20261017/implementation/api-validation/validation-summary.json)
- [最终独立语义复核](../tasks/document-understanding-20261017/implementation/final-semantic-review.json)

本目录按仓库本地 issue tracker 约定保存，可随仓库提交，目前未提交。原始请求、模型响应和测试日志位于被 Git 忽略的 `.scratch/tasks/`，仅保存在当前本地工作区；需要转移工作区时应另行保留原始证据。本目录的各任务也写有问题摘要，避免只靠原始证据链接理解问题。
