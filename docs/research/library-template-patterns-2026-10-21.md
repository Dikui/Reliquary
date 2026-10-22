# 精简资料、来源与后续补充：三种产品做法

调研日期：2026-10-21。范围：Readwise Reader、Raindrop.io、Capacities 的官方文档。仅作为现有 Reliquary 的规则参考，不评选替代程序，未进行账号内实测。下文区分官方事实、未知事项和设计推论；不代替用户决定接收标准。

## 对比结论

| 观察点 | Reader | Raindrop.io | Capacities |
| --- | --- | --- | --- |
| 简短内容与笔记 | 原文、摘要、文档笔记、高亮可分别保存 [R1] [R2] | 链接、描述、笔记、高亮分别管理 [D1] [D2] | 链接对象可先保存，随后加笔记或分析 [C1] |
| 资讯接收 | Feed 接收订阅，选中后进永久 Library [R3] | RSS 经集成保存为书签 [D3] | 可导入 Reader 资料与后续新增高亮 [C2] |
| 同来源后续处理 | 重存 URL 去重；原文刷新另有约束 [R4] | 规范化 URL 查重；可重新提取预览 [D4] [D1] | Retry 可重新分析；重复 URL 合并规则未知 [C1] [C3] |
| 模板参照 | 可自定义摘要提示词 [R5] | 描述和笔记可编辑 [D1] | 模板预填属性与正文块 [C4] |

## 1. Reader：订阅流与长期收藏分开

**官方事实。** Feed 接收 RSS 等自动推送，Library 保存主动选择长期留存的内容；Feed 条目可以移入 Library。RSS 可能只有摘要或首段，官方提供针对这类 feed 的重新解析入口。因此，至少订阅接收层可以出现不完整内容，不能据此推出所有保存入口均接受任意空正文。[R3] [R6]

摘要、文档笔记和正文是不同数据；笔记可以写在文档层，也可附在高亮上。API 同时提供来源 URL、发布日期、保存时间、更新时间；PATCH 可直接修改摘要和笔记。可自定义摘要提示词，但 Feed 默认不自动生成摘要，不能把“接收资讯”与“已完成提炼”视为同一步。[R1] [R2] [R5]

**更新边界。** Parsing FAQ 说，同 URL 再保存会命中已有文档并把它移到 Library 顶部；带不同跟踪参数的 URL 可能产生副本。对于“源网页更新后，刷新已经保存的原文”，该 FAQ 的方案是删除后重存，并明确会删除关联高亮和笔记。这不适用于摘要、笔记等可直接修改的字段。该页也建议对不完整 Feed 内容用浏览器扩展重存，但没有完整解释它与已存原文去重的所有交互，不能据此保证“摘要条目重存即升级为全文”。[R4]

## 2. Raindrop.io：书签记录与网页副本各有职责

**官方事实。** 保存链接会提取标题和描述；用户可编辑描述，并写独立 Markdown 笔记。高亮支持保存关键段落和附注。RSS 可以经 IFTTT 自动变成书签，或从外部阅读器一键保存；这与 Reader 内置 Feed 的方式不同。[D1] [D2] [D3]

去重先清理协议、www、尾斜杠、跟踪参数和 fragment 等差异，再比较 URL。一个书签只属于一个 collection；为了放入第二个 collection 再保存，会创建另一份。网页归档则保存收藏时的页面，原页变化后副本保持原样。API 区分记录创建/更新时间和归档创建时间，不能把它们直接当作原文发布日期。[D4] [D1] [D5] [D6]

**后续补充。** Refresh preview 会从原页重新提取内容；API 也能按既有 ID 改描述、笔记，并请求重新解析。**未知：**这些操作是否更新归档快照、如何处理已失效的高亮定位、重复书签如何合并各自笔记，所查文档没有明确保证。[D1] [D7]

## 3. Capacities：先留线索，再增加分析

**官方事实。** Weblink 是可加标签、笔记并被其他对象引用的记录。它有原 URL、描述和笔记；在桌面端主动分析后可产生摘要、关键词与保存的页面内容。抓取失败仍可保留链接并手写笔记。原网页、保存的正文和生成摘要以不同视图呈现；高亮可回到原段落。[C1]

模板用于预填某类对象的属性和正文块。Readwise 集成支持选定资料导入，后续新增高亮可以继续进入；对象属性及已有高亮的后续修改不持续同步。这说明“持续补充”需要逐字段说明，不能仅标成“支持同步”。[C4] [C2]

**后续补充。** 页面变化后可用 Retry 重新分析；即使内容未变化，高亮重新定位也不保证成功。扩展会提示链接已保存，但**未知：**URL 如何规范化，重复保存是否覆盖正文或追加笔记，缺少完整官方说明。对象 Created at 也不能等同于源页面发布日期。[C1] [C3] [C5]

## 可以带入 Reliquary 讨论的推论

以下都是待确认建议，不是已接受需求，也不预设库数量、模板数量或新增字段。

- **接收与提炼可分开。** 先保存摘要/片段及已知来源，再判断是否提炼或补全。是否需要独立订阅暂存区，仍由用户决定。
- **区分来源内容、摘要与个人补充。** 更新原文时应明确哪些可以重算，哪些必须保护；不一定要拆成多个库。
- **同 URL 不是同一种操作。** 重复投递、补充全文、源页面改版、追加个人笔记需要分别约定结果。去重不应自动被理解为丢弃新信息。
- **用可用性衡量摘要。** 用户的技术选型场景需要具体用途、特点和后续调研线索。外部产品提供存储方式，不能替 Reliquary 决定怎样的摘要才有用。

## 后续需要用户决定

1. 长期默认只留精简记录，还是允许精简记录与原文一起保留？个人笔记是否采用不同规则？
2. 什么信息足以支持“值得继续研究”的初筛？缺少来源时，名称和搜索词能否作为线索？
3. 同 URL 新提交带来更多内容时，默认补充、替换还是保留版本？个人笔记和已选重点如何保护？
4. 新鲜资讯全部长期留存，还是经过选择后留存？这与摘要不完整是否接收是两个问题。

<!-- 官方来源引用定义 -->

[R1]: https://readwise.io/reader_api "Reader API：正文、摘要、笔记、来源与时间、更新接口"
[R2]: https://docs.readwise.io/reader/docs/faqs/highlights-tags-notes "Reader：高亮、标签与笔记"
[R3]: https://docs.readwise.io/reader/docs/faqs/adding-new-content "Reader：Feed 与 Library"
[R4]: https://docs.readwise.io/reader/docs/faqs/parsing "Reader：去重、原文保存与刷新"
[R5]: https://docs.readwise.io/reader/docs/faqs/ghostreader "Reader：自定义摘要与 Feed 限制"
[R6]: https://docs.readwise.io/reader/docs/faqs/feed "Reader：不完整 RSS 内容"
[D1]: https://help.raindrop.io/bookmarks "Raindrop：书签描述、笔记、多个 collection、刷新预览"
[D2]: https://help.raindrop.io/highlights "Raindrop：高亮与附注"
[D3]: https://help.raindrop.io/integrations/rss "Raindrop：RSS 集成"
[D4]: https://help.raindrop.io/duplicates "Raindrop：URL 规范化与查重"
[D5]: https://help.raindrop.io/web-archive "Raindrop：网页归档"
[D6]: https://developer.raindrop.io/v1/raindrops "Raindrop：记录字段和归档时间"
[D7]: https://developer.raindrop.io/v1/raindrops/single "Raindrop：按 ID 更新与重新解析"
[C1]: https://docs.capacities.io/reference/basic-types/weblinks "Capacities：链接、摘要、原文、笔记与重新分析"
[C2]: https://docs.capacities.io/reference/integrations/readwise "Capacities：导入和增量同步边界"
[C3]: https://docs.capacities.io/reference/web-extension "Capacities：链接采集和已保存提示"
[C4]: https://docs.capacities.io/reference/templates "Capacities：模板预填"
[C5]: https://docs.capacities.io/reference/properties "Capacities：对象属性与创建日期"
