# Reliquary UI behavior

## Product context

Reliquary serves a single user collecting source material and maintaining entity profiles. The supported locales are zh-CN and en-US. Existing locale files own product copy. Light and dark themes retain the same hierarchy. New controls target WCAG 2.2 AA.

## Business-context sources

| Scope | Source |
| --- | --- |
| Current entity terminology and component scope | CONTEXT.md and docs/adr/0009-kb-collect-store-retrieve.md |
| Shared entity identity and article attribution | docs/adr/0002-global-entity-registry.md and the task 01 entity API contract |
| Features retained during staged implementation | .scratch/kb-reshape/spec.md and the explicit task 02 request |
| Single-user access | docs/adr/0007-personal-single-user.md |
| Imported personal documents and deletion | docs/adr/0013-user-documents-imported.md |

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Table Selection | DocumentBatchBar and EntryBatchDialog | Server batch preview and fixed membership at submit | Checked loaded rows or all matching results; choose scope before action and preserve list filters | Filter tests, frontend checks and browser batch flow |
| Select/Listbox | TDesign Select and TagInput | Existing TDesign installation and locale provider | Filtered, multiple and tag input | Frontend type check and component tests |
| Date | TDesign DatePicker for API Key expiry | ApiIntegrationSettings.vue and active locale | Local minute precision, Unix seconds on submission | API Key expiry tests |
| Form | TDesign inputs and application validation | Existing library editor and settings forms | Create and edit | Editor validation tests |
| Textarea autosize | SafeTextarea wrapping TDesign Textarea | SafeTextarea.vue and utils/textareaAutosize.ts | Boolean autosize or minRows/maxRows | Actual component mount and height regression tests |
| Scrollbar | Global application theme stylesheet | UX-CONTRACT.md and frontend/src/assets/theme/theme.css | Stable gutter for bounded regions | Premium static audit |
| Toast | TDesign MessagePlugin | Existing settings workflows | Success, warning, error and information | Frontend component tests |
| Model tiers | ModelTierEditor and ModelSelector | Model routing API and existing model catalogue | Independent library stages; shared space embedding | Model preview tests and frontend checks |
| Merge suggestions | MergeSuggestions and shared useConfirmDelete | Entity API and ADR 0002 | Pending, accepted and rejected | Backend lifecycle and frontend component tests |
| Model call budgets | ModelCallSummary | Processing span model_calls | Processing details | Budget validation and component tests |
| CRUD | Library editor, entity API and existing document detail | Entity profiles and library management | Entity merge refreshes the owning list | Entity lifecycle and editor tests |
| Repost sources | RepostSources | Entry detail API | Document preview | Shared component and full SFC regression tests |
| Article classification | EntryClassification | Article content_type, structured_summary, entities and topics | Compact list and complete detail | Actual SFC render, link safety and locale tests |
| Image preview | ImagePreview wrapping TDesign ImageViewer | SearchImage and picture-preview | Authenticated search Blob and existing picture URL | Real browser Escape, opening focus and trigger focus restoration |

## Flow ledger

| Operation | Pending | Success | Failure recovery |
| --- | --- | --- | --- |
| View entity | Clear the previous article list and show loading | Show type, aliases and articles mentioning the entity in the single library | Keep selection, show inline error and allow refresh |
| Filter entity list | Debounce search and invalidate older requests | Show the filtered list with explicit load more | Show inline error and allow refresh |
| Merge entity | App-owned dialog names the retained entity and space-wide consequences; disable duplicate requests | Refresh the entity list and announce success | Keep selected target and show inline error |
| Accept merge suggestion | Shared confirmation names both entities and space-wide consequences | Refresh the list, retain filter and announce success | Keep list and show inline error |
| Reject merge suggestion | Disable duplicate requests | Keep entities separate and refresh suggestions | Keep list and show inline error |
| Rebuild entity name embeddings | Disable duplicate starts; poll active runs every five seconds | Show status and processed/total counts | Preserve failure reason and retry controls |

## Dataset and draft state

The library catalogue permits an empty list and keeps library creation available. Empty libraries, including historical system libraries, use the existing delete confirmation and server-side emptiness check. Successful deletion refreshes the catalogue; deleting the final library clears its editor. Startup and registration do not recreate deleted libraries. The four template behaviors remain available when creating a library. Source: ADR 0014, revised 2026-10-21.

Entity relationship views switch between List and Graph inside the existing entity profile. List remains the default and owns relationship deletion. Graph loads only after its first activation and uses the existing graph search endpoint with explicit library IDs, entity ID, one to three hops, relationship types, and a limit of 20, 50, or 100 (default 50). Filters and the chosen view are transient entity-workbench state, as with the existing entity filters; switching views preserves graph filters. Queries replace the current local graph, cancel previous requests, and ignore older responses. Hiding or leaving the graph cancels pending queries. Reopening it refreshes current data. Layout and viewport gestures never issue queries.

Graph edges preserve subject-to-object arrows and individual relationship IDs across libraries. The selected node or edge highlights its related elements and exposes details in a non-modal side panel, which stacks below the graph in narrow containers. Source links reuse the existing entry/SourceLocation navigation. An entity action may either recenter the graph or open the existing list profile. Graph results expose separate relationship/document truncation notices and global/involved pending-sync record counts; empty or truncated results never claim that no relationship exists. A graph HTTP 503 is shown locally rather than as a whole-application outage. Canvas controls include zoom and fit, keyboard panning, and native buttons for browsing nodes and edges. Theme colors and typography are read from the existing runtime theme; no graph palette or typography system is introduced. Source: ADR 0014 and docs/search-api.md.

Entity and suggestion lists use explicit load more with fifty and twenty rows per request. Filter changes invalidate older results. Entity details show the shared identity and articles in the single library. Entity workbench filters and selected section are transient within the article view; its main documents/entities tab remains in URL state. Every article list filter persists in the URL: source_kind, content_type, starred, incomplete, file_type, parse_status, source, keyword, start_date and end_date. Default values are omitted. Refresh and history navigation restore the controls and results together, retaining unrelated query parameters. The source category reuses the search source values and labels. Filters combine in both cards and rows. Image status supports all materials, missing images and no missing-image records. The filter panel uses aligned, wrapping controls; review and rebuild actions remain separate from the filter grid. Switching filters resets paging and clears hidden selections. Composing Chinese input does not trigger entity search. Clearing search refreshes immediately. Merge target options also use explicit load more and load across the entire space.

Model tiers and article processing configuration save in the library settings page. The server validates model compatibility. Leaving changed model tiers uses the shared application confirmation dialog. Failed saves preserve drafts and inline errors.

## Layout and interaction

Settings content owns vertical scrolling. Long forms keep their natural height. New controls use TDesign keyboard behavior, visible focus, disabled and busy states, and app-owned dialogs. Native entity selection buttons have visible focus and hover states. Entities and article titles wrap and remain readable in narrow views. Autosizing textareas use SafeTextarea and cancel pending measurements on unmount.

The settings-content container stacks rows at 720px or less. Shared settings-layout.less owns that breakpoint. Existing rules below 640px remain in place.

Document details fill the viewport at widths of 720px or less. Desktop drawer width remains the user's preference. Narrow details hide the desktop resize handle. Long source links wrap; code and tables may keep bounded local scrolling.

Article content types filter the server list and clear hidden selections. All articles show the complete Markdown body alongside their original link, structured summary, entities and topics. Selecting an article entity opens its profile in the existing entity workbench. Reprocess queues the complete article processing call and polls parse_status while active, replacing the displayed classification, summary and relations on completion. Failed reprocessing keeps the readable server reason visible and allows another attempt.

Source lists show publisher, original URL, publication time or a missing marker, and matching method. URLs use the shared absolute HTTP(S) URL helper; unsafe values remain escaped text. Opening a different document clears old source data and ignores earlier responses.

API Key expiry uses TDesign DatePicker and the active locale. Values retain local date-time at minute precision and convert to Unix seconds on submission. Clearing expiry omits expires_at_unix.

Browser inspection is handled by the user's separate task. This task records static, component, localization, type-check and build evidence without claiming browser verification.

Knowledge search lives at /platform/search. TDesign multi-select controls filter content types, entities by ID, topics by ID, and article, user document or report sources. Publication date filters exclude articles without a publication date and show the excluded count. Each result contains one article with at most three matching snippets, summary and source links. Similar reports collapse by default and expand within their leading article; a request switch disables collapse. Search requests invalidate older responses, retain readable errors and expose a retry action. The global search shortcut and API integration Playground share the article search API and supported parameters.

Topic management lives at /platform/topics. Creation and description edits start historical assignment, with processed/total article counts and failed article/batch counts on each topic. Failure reasons remain visible, and retry targets failed batches. Progress polling stops on completion and route teardown; leaving the page does not stop server work. Duplicate-name failures preserve the editor draft. Delete confirmation names the topic and explains removal of article associations. Failed deletions keep the confirmation open.

Article stars wait for server confirmation before updating the card, row or detail. A failed update retains the original state and exposes an inline retryable error. List, search and Playground filters support all, starred and unstarred articles; search persists this state in its URL. Changing a star removes an article from an incompatible active list filter.

The article detail uses EntryArticleJudgments for content type and topic corrections. Changing a content type preserves the complete Markdown body, images and body index. Failure keeps the correction dialog open. Corrected types remain visible after reprocessing. Topic corrections save selected IDs and reload current associations. Busy corrections disable reprocessing. Review offers approve, retry under the current library policy, and one confirmed reject action that deletes the intake record, original text and exclusively owned attachments. Retry remains available when the original URL is missing. Model rejection keeps the material pending; only confirmed user rejection deletes it. Shared pending and inline failure states apply.

The main navigation groups existing entries into Materials, Needs attention, Diagnostics and Configuration. Needs attention opens /platform/entries?tab=reviews and retains only intake review and missing images. Diagnostics opens /platform/diagnostics and also groups the existing evaluation and model-call pages. Existing contextual entries remain available. The entity entry opens /platform/entries?tab=entities. Settings retain library, models, file library status, operation logs, API integration and runtime queues. File library status lives at /platform/settings?section=vault.

Follow controls share server state in entity profiles, topic management and article detail relation labels. Successful mutations update all controls and the sidebar count; failures preserve the prior follow state and show an inline error with retry. Article stars retain their bookmark behavior. Entity merges transfer follows to the retained entity and deduplicate them; topic deletion removes its follow.

The follow feed lives at /platform/follows and shows completed external articles newest collected first, with each article's matching followed entities and topics. It excludes personal documents, reports and both pending review kinds. New markers use the later of feed qualification and association creation, independent of collection time. Historical topic backfill links do not produce new markers or unread counts. Adding a follow does not reset old articles' entrance times. Object filters and the existing similar report switch keep the feed's original new-marker snapshot across refreshes. Each folded article retains its own matches and new marker. Successful first loading records the captured opening time; loading failures do not advance it. The sidebar count tracks distinct unseen articles and refreshes on navigation, visibility and every thirty seconds. Mark all viewed advances the checkpoint and clears new markers. MCP reads share feed data without changing that checkpoint and paginate through a fixed initial window using cursor and next_cursor. External AI owns brief writing and uses write_report to save it.

The operation log lives at /platform/settings?section=operation-logs and lists user actions newest first, with exact local timestamps, displayed timezone, resource ID and before/after values. The previous /platform/operation-logs route redirects to settings and preserves query parameters. Operation type and page are stored in the URL. Filter changes reset the page and invalidate earlier requests. Refresh and failed-read retry preserve readable records. Log entries are read-only.

Embedding model settings persist supports_images and the Jina multimodal format and send them through the model connection test. The library model tab allows changing the embedding model and exposes all-vector rebuilding. Its progress is loaded from the API and polled every two seconds while active. Pause, resume and failed-item retry preserve successful checkpoints; errors retain the latest readable progress. Changing model identity requires a new run. Retrieval uses only the current model's vectors and keeps keyword search available while replacements are built. SearchImage loads image snippets in both full and global search through the existing authenticated attachment request, cancels stale requests and releases Blob URLs on replacement or unmount.

Pending reviews live at /platform/entries?tab=reviews. TDesign tabs divide intake review and missing images (缺少图片). The missing-images label is shared by article badges, search and detail; it is independent of processing status. Reprocessing is selected through the shared bulk flow after choosing checked rows or all matching materials. Failed status limits the scope to failures; completed and approved cancelled materials participate only when included in the chosen filters. The preview and model-cost warning precede confirmation. Reprocessing preserves missing-image markers and records; failed intake reviews run again before processing. Each tab uses twenty records per page. Switching tabs invalidates earlier responses. Refresh errors retain the current list, and mutations refresh the owning page and article list. A removed final record moves to the previous valid page.

ReviewActions owns intake review actions in both the pending list and article detail. Missing source links show add URL and review again, or collect without URL. Collecting without a URL requires confirmation of the source unverified marker. Failed operations retain the inputs and confirmation with an inline error. Materials retain their full Markdown text and supplied images until the user confirms rejection/deletion.
LibraryReviewAction owns confirmed batch review submission and the resulting queue link. The intake tab offers review of the selected library's pending materials. It requires exactly one non-generated library, describes the scope and model cost, and retains the dialog on submission failure. It does not automatically delete materials that fail model review.

Article cards, rows, detail and the full search page share EntryClassification for the source unverified marker. The global search hook preserves the same field, and ResultItem displays its localized badge. The detail loading hook preserves that field , resets them on document switches, and ignores older responses. Content arrives only from external tools through HTTP and MCP ingest interfaces. The article page has no URL import, file upload, manual creation or manual editing controls. Legacy manual articles and their drafts remain readable in the detail drawer. ReparseConfirmDialog owns existing article reprocessing configuration. Legacy RSS and web channels retain their display labels. Remote image references display a localized missing-file link without loading or probing the source. Supplied attachments, inline images and same-origin images remain viewable. External Agent integration guidance states that model intake review applies to HTTP and MCP collection and that rejected content returns pending_review with a reason.

## 界面命名与样式约定

Reliquary 保留现有页面结构、色彩、字体和交互。产品显示名称为 Reliquary；Go/TypeScript 导出符号按语言惯例使用 Reliquary。登录页与侧栏共用 frontend/src/assets/img/reliquary.svg。

唯一库的根面包屑使用 library.title，中文显示“库”，英文显示“Library”，不随数据库中的库名称变化。文章、待处理、实体及设置背景共用此名称。

实体页面包含实体档案和待合并建议。实体详情以类型、别名和提及该实体的文章为主要信息结构，文章可通过原有文章详情入口打开。实体类型固定为公司、人物、产品、项目、组织、其他。模型配置、合并操作和名称向量重建沿用现有 TDesign 组件与中英文文案。

运行时样式以 frontend/src/assets/theme/theme.css 为唯一色彩与圆角来源，经 frontend/src/main.ts 加载后供 TDesign 组件及 var(--td-*) 使用。字体由 frontend/src/composables/useFont.ts 管理，保留既有中文与英文选择。新增区域不另建色彩、字体或阴影体系。滚动条使用全局主题变量，所有新增溢出区域自动继承。

实体列表使用显式加载更多，实体详情保持自然高度。窄屏把实体列表与档案改为上下排列，长实体名、别名和文章标题换行。实体合并前明确保留实体与相关文章影响，待合并建议通过共享确认对话框处理。

模型档位在设置中心的库设置中配置，使用 ModelTierEditor 与原有 ModelSelector。库目录内按选中库编辑标准、适用模型与分块参数；参考文档和个人产出隐藏实体身份模型与图谱设置，AI产出隐藏审核与内容处理设置。共享嵌入页签仅配置空间向量模型。每一行显示用途、选择和生效来源，不暗示其他库提供默认值。恢复建议标准仅改草稿，保存后生效；切库和离开前沿用未保存确认，失败保留草稿。父子分块开关按模板锁定。待审区显示选中库重新审核时使用的当前标准。资料和实体页面使用 /platform/entries；内容由外部工具通过收录接口送入。模型调用继续使用共享 ModelCallSummary 表格。

主导航按资料管理、待处理、辅助诊断、系统配置分组。待处理直达原待审区，仍只有收录待审与缺少图片；实体入口打开现有实体档案页签。设置中心提供库、模型、文件库状态、操作日志、API 集成和运行队列入口，沿用原有桌面外观。较窄的桌面窗口先减少内容左右留白，再收窄侧栏；内容使用命名容器 settings-content，实际可用宽度不超过 720px 时，标签与控件上下排列。断点与行布局由 frontend/src/assets/settings-layout.less 统一维护，控件可收缩，表格只在自身区域横向滚动。既有 640px 以下规则继续保留。

自动增高文本框共用 SafeTextarea，保留 TDesign 外观与输入交互，项目负责可取消的安全高度测量。API Key 有效期使用 TDesign 日期时间选择器与全局中英文语言配置，保持原本本地时间、分钟精度与 Unix 秒提交方式。

Embedding 模型编辑提供支持图片开关和 Jina 请求格式，模型卡显示图片能力。库模型页签使用 EmbeddingRebuild 展示全部向量重建的总数、完成数、失败数及暂停、继续、失败重试动作。检索结果与全局快捷搜索共用 SearchImage，沿用附件鉴权与主题变量，缩略图保留固定区域并提供加载和失败状态。大图预览共用 ImagePreview 与 TDesign ImageViewer，打开移入焦点，Esc、关闭按钮和遮罩关闭后返回触发元素。新增设置沿用现有 TDesign 行布局，不另建外观体系。

文章列表和详情共用 EntryClassification 展示内容类型、处理方式、概述、要点、关键名词、实体和主题。列表用紧凑字号保留三部分摘要，卡片随内容自然增高。实体是可聚焦的按钮，主题沿用现有轻量标签。所有文章显示可换行的安全原文链接和已保存的 Markdown 正文。

知识检索页使用 /platform/search。查询框与 TDesign 多选控件提供内容类型、实体、主题和来源筛选，日期控件限制发布时间；相似报道默认折叠，可关闭或逐组展开。每篇文章只展示一条主结果，最多三个命中片段，文章提供原文入口。缺失发布时间的排除数量在结果区说明。全局搜索继续作为快捷入口，API 集成页的 Playground 使用相同参数。筛选与结果沿用现有主题变量，窄屏自然换行。

主题管理使用 /platform/topics，侧栏提供直接入口。主题名称、说明、文章数和历史归入进度在同一记录中显示，失败批次保留可读原因与重试操作。操作日志在 /platform/settings?section=operation-logs，原 /platform/operation-logs 入口重定向到设置并保留筛选参数。日志按时间倒序显示修改前后值，并说明本地时区。两页沿用既有 TDesign 控件、主题变量、自然高度记录和窄屏换行。

文章卡片、列表行与详情共用 EntryStarToggle。详情通过 EntryArticleJudgments 修改内容类型与主题，内容类型仅作筛选标签，修改时保留正文、索引与图片。文章列表、检索与 API Playground 的星标筛选都提供全部、已星标、未星标三个状态。

Personal documents arrive through ingest with kind=mine. Pending intake and missing-image lists show the personal document source badge. Missing URLs do not expose the article missing-link actions or prevent approval. Every content type retains full text. Cards and rows use personal document wording that names the file, attachments and index and explains reimport for changes. The detail header uses PersonalDocumentDelete with the shared DocumentDeleteConfirm. Article and personal document card and row menus close before opening that shared confirmation outside the Popup. The modal attaches to body, shows its complete content and blocks underlying entry clicks with its overlay. During deletion, both the confirmation and the enclosing detail drawer reject overlay, Escape and close-button dismissal; their callbacks also reject closing. Duplicate submission is disabled. Failures retain the open dialog, readable error and retry action. Closing becomes available when the request finishes. Successful deletion closes the confirmation and refreshes the article list while preserving filters; detail deletion also closes the drawer and refreshes pending lists. Personal document details offer no metadata editing or reprocessing. File library status describes imports and does not show a scan timestamp for mine/.

## Ingest failure diagnostics

Source: .scratch/agent-operations-diagnostics/spec.md and internal/types/ingest_diagnostics.go. The read-only /platform/diagnostics page lists technical ingest failures, with URL-backed committed filters and pagination. Selecting a failure preserves the list query in the URL. List, detail and material requests ignore stale responses and teardown. Refresh failures retain the current readable list. Filters use explicit apply; clear immediately applies the new filter. TDesign owns date entry and localization.

Details distinguish unknown outcomes, partial inputs, unavailable materials and expiry. Raw text is escaped and read in server-defined Unicode segments. Image reads use the authenticated request owner, never a public file path; Blob URLs are released when replaced or unmounted. Reading does not replay, modify or delete content. Recovery entry/request associations are links, not claims of completed repair. Existing failed processing, timeline, model calls, queues, operation log, vault and evaluation owners remain accessible. The default retention hint is 7 days / 512 MiB and explains capacity eviction. Visual intent and token ownership are documented in DESIGN.md.

## Bulk material management

The materials toolbar always offers the shared EntryBatchDialog, including when no loaded row is checked. DocumentBatchBar opens the same dialog for checked rows. Scope selection is step one; action selection and preview are step two. Both steps retain a scope summary of the captured filters and the exact server batch count, including unloaded and uncollapsed materials. Checked scopes distinguish available and unavailable IDs and never expand to all matching filters. Count failures stay visible with retry; loading or failed counts block advancement. Scope count requests are independent of action previews and ignore stale responses. Returning to scope preserves this summary but invalidates the action preview and confirmation. Checked rows and all matching server results are separate scopes. All-match selection includes unloaded results and fixes membership at confirmation submission. Entity IDs, topic IDs and publication dates join existing URL-backed document filters. Entity profiles and topic records link to the matching documents view. Publication date filtering excludes missing dates. Filter or library changes clear hidden selections.

Delete, reprocess, review, star changes, content type corrections and topic add/remove share this dialog. The materials toolbar has no separate whole-library review or rebuild shortcuts. All-match scope preserves every current list filter. To reprocess a library, filter by that library; add failed status to target only failures. The intake page retains its explicit pending-review context. Content types come from a single library catalogue. Preview shows eligible, skipped and blocked counts, reasons and samples; a named scope and explicit confirmation precede submission. Delete explains irreversible removal. Reprocess uses current library policy and excludes generated output, whose index maintenance remains separate. Review preserves source text and never performs user rejection deletion.

Accepted submissions show pending/running state, then server terminal counts and paginated item results. No submission toast claims completed success. Errors preserve the dialog and results with refresh recovery. Only failed items can be retried. Pending requests prevent duplicate mutations; generation checks reject stale responses; closing and teardown stop client polling without stopping server work. TDesign owns dialog focus, Escape, selects, date entry and pagination. Both locales and existing semantic theme tokens apply.

Batch submissions retain batch_id in the documents route. The result link and a page reload reopen server status in the root EntryBatchDialog. Review shortcuts hand off to that same root result owner; closing the dialog does not orphan its route.
