# 05：核对两篇资料的内容分类口径

Status: needs-triage
Priority: P2
Recorded: 2026-10-17

## 观察与待确认事项

最终真实 API 结果有两篇超出此前预标注的可接受分类集合：

- Inside our approach to the Model Spec：模型选择“新闻”。
- How to connect AI usage to business value：模型选择“公告”。

这些标签来自实际返回，未人工修改。但预标注不是人工专家金标，因此目前属于需要确认的分类差异，不能直接认定模型分类必错。分类质量未列入此次四项实现验收。

## 目标与范围

先核对库中内容类型定义、文章主要用途与预标注口径，确定是否确有错误；确认后再作最小修复。内容类型会影响图谱抽取开关，需一并检查实际影响。

## 验收

- 两篇有可解释、符合既定类型定义的分类结论。
- 若模型或预标注有误，明确修正哪一方，保留判断依据。
- 确认分类后的图谱开关符合库配置，不为保留关系而强行改分类。

## 证据

[最终 API 汇总](../../tasks/document-understanding-20261017/implementation/api-validation/validation-summary.json)中的 `classification_outside_preaccepted_documents`。原始材料仅在当前本地工作区。

## Comments

2026-10-17：登记为待核对，没有更改分类提示、分类目录或历史分类。
