# Reliquary UI behavior

## Product context

Reliquary serves a single user collecting source material and maintaining entity profiles. The supported locales are zh-CN and en-US. Existing locale files own product copy. Light and dark themes retain the same hierarchy. New controls target WCAG 2.2 AA.

## Business-context sources

| Scope | Source |
| --- | --- |
| Current entity terminology and component scope | CONTEXT.md and docs/adr/0009-kb-collect-store-retrieve.md |
| Shared entity identity and article attribution | docs/adr/0002-global-entity-registry.md and the task 01 entity API contract |
| Features retained during staged implementation | .scratch/kb-reshape/spec.md (local only) and the explicit task 02 request |
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

Relation type management opens from the entity graph toolbar, not library settings. The dialog edits one library at a time and submits only relation types. Types can accumulate automatically; an empty list explains that no setup is required. Closing or switching libraries with unsaved edits requires confirmation. Merging requires two enabled types and explicit confirmation; failures retain the selection for retry. Successful edits and merges refresh the graph catalog and results, removing filters for types that no longer exist.

Entity and suggestion lists use explicit load more with fifty and twenty rows per request. Filter changes invalidate older results. Entity details show the shared identity and articles in the single library. Entity workbench filters and selected section are transient within the article view; its main documents/entities tab remains in URL state. Every article list filter persists in the URL: source_kind, content_type, starred, file_type, parse_status, source, keyword, start_date and end_date. Default values are omitted. Refresh and history navigation restore the controls and results together, retaining unrelated query parameters. The source category reuses the search source values and labels. Filters combine in both cards and rows. The filter panel uses aligned, wrapping controls; review and rebuild actions remain separate from the filter grid. Switching filters resets paging and clears hidden selections. Composing Chinese input does not trigger entity search. Clearing search refreshes immediately. Merge target options also use explicit load more and load across the entire space.

Model tiers and article processing configuration save in the library settings page. The server validates model compatibility. Leaving changed model tiers uses the shared application confirmation dialog. Failed saves preserve drafts and inline errors.

## Layout and interaction

The Swiss platform sidebar preserves the existing route destinations during the
page migration. Materials opens the library catalogue; each library opens the
entry page with its `libraries` query. Entities and pending confirmation keep the
existing `tab=entities` and `tab=reviews` routes. Settings still opens the existing
settings surface. Page scroll owners remain responsible for their content; all pages use the shared Swiss tokens.

Navigation uses native links, persisted collapse state and a 640px mobile
breakpoint. The shared Reka drawer traps focus, closes on Escape, navigation or
viewport changes, and restores the trigger. Opening the global command palette
closes the navigation drawer without moving focus away from the palette.
Counts come from the library catalogue, follow state, pending intake and merge
suggestion APIs, and the ingest failure list total. Pending confirmation adds
intake to all pages of pending merge suggestions in each library. Missing or
failed data hides the affected count rather than presenting a partial total or
invented zero. Route changes, visibility and the 30-second navigation refresh
keep displayed counts current. Source: existing frontend API contracts.

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

The follow feed lives at /platform/follows and shows completed external articles newest collected first, with each article's matching followed entities and topics. It excludes personal documents, reports and intake review. New markers use the later of feed qualification and association creation, independent of collection time. Historical topic backfill links do not produce new markers or unread counts. Adding a follow does not reset old articles' entrance times. Object filters and the existing similar report switch keep the feed's original new-marker snapshot across refreshes. Each folded article retains its own matches and new marker. Successful first loading records the captured opening time; loading failures do not advance it. The sidebar count tracks distinct unseen articles and refreshes on navigation, visibility and every thirty seconds. Mark all viewed advances the checkpoint and clears new markers. MCP reads share feed data without changing that checkpoint and paginate through a fixed initial window using cursor and next_cursor. External AI owns brief writing and uses write_report to save it.

The operation log lives at /platform/settings?section=operation-logs and lists user actions newest first, with exact local timestamps, displayed timezone, resource ID and before/after values. The previous /platform/operation-logs route redirects to settings and preserves query parameters. Operation type and page are stored in the URL. Filter changes reset the page and invalidate earlier requests. Refresh and failed-read retry preserve readable records. Log entries are read-only.

Embedding model settings persist supports_images and the Jina multimodal format and send them through the model connection test. The library model tab allows changing the embedding model and exposes all-vector rebuilding. Its progress is loaded from the API and polled every two seconds while active. Pause, resume and failed-item retry preserve successful checkpoints; errors retain the latest readable progress. Changing model identity requires a new run. Retrieval uses only the current model's vectors and keeps keyword search available while replacements are built. SearchImage loads image snippets in both full and global search through the existing authenticated attachment request, cancels stale requests and releases Blob URLs on replacement or unmount.

Pending reviews live at /platform/entries?tab=reviews and contain legacy materials awaiting a manual decision. New intake does not enter this queue. Missing images do not create a workflow, badge or filter. Reprocessing uses the shared bulk flow for selected rows or matching materials, including AI output. Refresh failures retain the list; resolving its last record returns to the previous valid page.

ReviewActions owns manual collection and rejection in the pending list and article detail. Source links remain optional: users can add a URL and collect, or confirm collection without one while retaining the source-unverified marker. Rejection still requires confirmation. Failures retain inputs and the open confirmation. The interface offers no library-wide or batch review action; the removed HTTP route and batch request validation prevent new review jobs. Historical jobs and results remain supported.

Article cards, rows, detail and the full search page share EntryClassification for the source unverified marker. The global search hook preserves the same field, and ResultItem displays its localized badge. The detail loading hook preserves that field , resets them on document switches, and ignores older responses. New content arrives only from external tools through HTTP and MCP ingest interfaces. The article page has no URL import, file upload or manual creation controls. Archived entries offer explicit body editing in the existing detail drawer. EntryContentEditor owns the complete Markdown draft, preview switch, save feedback and unsaved-change confirmation; DocContent owns rendering, attachment access and processing status. The editor submits the read body's hash and retains the draft on failure or conflict, with copy/reload recovery. Entry ID, source and title remain unchanged; saved edits confirm storage immediately and run indexing and enrichment in the background, without body history or persistent drafts. Source: ADR 0014, docs/ingest-api.md and the explicit web-editing request. Legacy manual articles and their drafts remain readable in the detail drawer. ReparseConfirmDialog owns existing article reprocessing configuration. Legacy RSS and web channels retain their display labels. Remote image references display a localized missing-file link without loading or probing the source. Supplied attachments, inline images and same-origin images remain viewable. External Agent integration guidance describes technical admission checks, immediate storage and independent background processing for HTTP and MCP collection.

## 界面命名与样式约定

Reliquary 保留现有页面结构、色彩、字体和交互。产品显示名称为 Reliquary；Go/TypeScript 导出符号按语言惯例使用 Reliquary。登录页与侧栏共用 frontend/src/assets/img/reliquary.svg。

主导航“库”进入 /platform/libraries，按现有库显示卡片；点击卡片进入 /platform/entries?libraries=<id>，只查看所选库的资料。资料页标题为“库 > 当前库”，可返回库卡片。实体和历史待审保留独立侧栏入口，不再在资料页顶部重复展示。资料页仅默认展开库、处理状态和内容类型；低频筛选收进“更多筛选”，有已应用的低频筛选时自动展开。

实体页面包含实体档案和待合并建议。实体详情以类型、别名和提及该实体的文章为主要信息结构，文章可通过原有文章详情入口打开。实体类型固定为公司、人物、产品、项目、组织、其他。模型配置、合并操作和名称向量重建使用共享组件与中英文文案。

运行时样式以 frontend/src/styles/tokens.css 为色彩、字体和圆角来源；frontend/src/styles/global.css 提供页面基础样式。字体由 frontend/src/composables/useFont.ts 管理，保留既有中文与英文选择。新增区域不另建色彩、字体或阴影体系。滚动条使用全局主题变量，所有新增溢出区域自动继承。

实体列表使用显式加载更多，实体详情保持自然高度。窄屏把实体列表与档案改为上下排列，长实体名、别名和文章标题换行。实体合并前明确保留实体与相关文章影响，待合并建议通过共享确认对话框处理。

模型档位在设置中心的库设置中配置，使用 ModelTierEditor 与原有 ModelSelector。库目录按选中库展示统一的只读入库规则，并编辑适用模型、分块参数、内容类型与关系类型；生成型也可配置这些处理能力。审核标准、来源门槛和按类型抽图开关不再提供编辑。共享嵌入页签仅配置空间向量模型。每一行显示用途、选择和生效来源，不暗示其他库提供默认值。切库和离开前沿用未保存确认，失败保留草稿。父子分块开关按模板锁定。待审区明确标为历史资料的人工处置入口。资料和实体页面使用 /platform/entries；内容由外部工具通过收录接口送入。模型调用继续使用共享 ModelCallSummary 表格。

主导航按资料管理、待处理和辅助诊断分组；单项待处理不再重复显示分组标题。设置仅保留侧栏底部一个按钮，点击直接打开原设置窗口；不显示头像、账号占位和快捷设置子菜单。资料页不再显示信息浮层、设置齿轮和重复搜索入口。实体名称向量维护面板默认折叠，操作与错误信息仍可查看。待处理直达原待审区，只保留收录待审；实体入口打开现有实体档案页签。设置中心提供库、模型、文件库状态、操作日志、API 集成和运行队列入口，沿用原有桌面外观。较窄的桌面窗口先减少内容左右留白，再收窄侧栏；内容使用命名容器 settings-content，实际可用宽度不超过 720px 时，标签与控件上下排列。断点与行布局由 frontend/src/assets/settings-layout.less 统一维护，控件可收缩，表格只在自身区域横向滚动。既有 640px 以下规则继续保留。

自动增高文本框共用 SafeTextarea，保留 TDesign 外观与输入交互，项目负责可取消的安全高度测量。API Key 有效期使用 TDesign 日期时间选择器与全局中英文语言配置，保持原本本地时间、分钟精度与 Unix 秒提交方式。

Embedding 模型编辑提供支持图片开关和 Jina 请求格式，模型卡显示图片能力。库模型页签使用 EmbeddingRebuild 展示全部向量重建的总数、完成数、失败数及暂停、继续、失败重试动作。检索结果与全局快捷搜索共用 SearchImage，沿用附件鉴权与主题变量，缩略图保留固定区域并提供加载和失败状态。大图预览共用 ImagePreview 与 TDesign ImageViewer，打开移入焦点，Esc、关闭按钮和遮罩关闭后返回触发元素。新增设置沿用现有 TDesign 行布局，不另建外观体系。

文章列表和详情共用 EntryClassification 展示内容类型、处理方式、概述、要点、关键名词、实体和主题。列表用紧凑字号保留三部分摘要，卡片随内容自然增高。实体是可聚焦的按钮，主题沿用现有轻量标签。所有文章显示可换行的安全原文链接和已保存的 Markdown 正文。

知识检索页使用 /platform/search。查询框与 TDesign 多选控件提供内容类型、实体、主题和来源筛选，日期控件限制发布时间；相似报道默认折叠，可关闭或逐组展开。每篇文章只展示一条主结果，最多三个命中片段，文章提供原文入口。缺失发布时间的排除数量在结果区说明。全局搜索继续作为快捷入口，API 集成页的 Playground 使用相同参数。筛选与结果沿用现有主题变量，窄屏自然换行。

主题管理使用 /platform/topics，侧栏提供直接入口，界面和交互以本文 Swiss topics migration 约定为准。操作日志在 /platform/settings?section=operation-logs，原 /platform/operation-logs 入口重定向到设置并保留筛选参数。日志按时间倒序显示修改前后值，并说明本地时区；继续沿用既有 TDesign 控件、主题变量、自然高度记录和窄屏换行。

文章卡片、列表行与详情共用 EntryStarToggle。详情通过 EntryArticleJudgments 修改内容类型与主题，内容类型仅作筛选标签，修改时保留正文、索引与图片。文章列表、检索与 API Playground 的星标筛选都提供全部、已星标、未星标三个状态。

Personal documents arrive through ingest with kind=mine. Pending intake and missing-image lists show the personal document source badge. Missing URLs do not expose the article missing-link actions or prevent approval. Every content type retains full text. Cards and rows use personal document wording that names the file, attachments and index and explains reimport for changes. The detail header uses PersonalDocumentDelete with the shared DocumentDeleteConfirm. Article and personal document card and row menus close before opening that shared confirmation outside the Popup. The modal attaches to body, shows its complete content and blocks underlying entry clicks with its overlay. During deletion, both the confirmation and the enclosing detail drawer reject overlay, Escape and close-button dismissal; their callbacks also reject closing. Duplicate submission is disabled. Failures retain the open dialog, readable error and retry action. Closing becomes available when the request finishes. Successful deletion closes the confirmation and refreshes the article list while preserving filters; detail deletion also closes the drawer and refreshes pending lists. Personal document details use the shared body editor and background processing status, including retry after failure. File library status describes imports and does not show a scan timestamp for mine/.

## Ingest failure diagnostics

Source: .scratch/agent-operations-diagnostics/spec.md (local only) and internal/types/ingest_diagnostics.go. The read-only /platform/diagnostics page lists technical ingest failures, with URL-backed committed filters and pagination. Selecting a failure preserves the list query in the URL. List, detail and material requests ignore stale responses and teardown. Refresh failures retain the current readable list. Filters use explicit apply; clear immediately applies the new filter. TDesign owns date entry and localization.

Details distinguish unknown outcomes, partial inputs, unavailable materials and expiry. Raw text is escaped and read in server-defined Unicode segments. Image reads use the authenticated request owner, never a public file path; Blob URLs are released when replaced or unmounted. Reading does not replay, modify or delete content. Recovery entry/request associations are links, not claims of completed repair. Existing failed processing, timeline, model calls, queues, operation log, vault and evaluation owners remain accessible. The default retention hint is 7 days / 512 MiB and explains capacity eviction. Visual intent and token ownership are documented in DESIGN.md.

## Bulk material management

The materials toolbar always offers the shared EntryBatchDialog, including when no loaded row is checked. DocumentBatchBar opens the same dialog for checked rows. Scope selection is step one; action selection and preview are step two. Both steps retain a scope summary of the captured filters and the exact server batch count, including unloaded and uncollapsed materials. Checked scopes distinguish available and unavailable IDs and never expand to all matching filters. Count failures stay visible with retry; loading or failed counts block advancement. Scope count requests are independent of action previews and ignore stale responses. Returning to scope preserves this summary but invalidates the action preview and confirmation. Checked rows and all matching server results are separate scopes. All-match selection includes unloaded results and fixes membership at confirmation submission. Entity IDs, topic IDs and publication dates join existing URL-backed document filters. Entity profiles and topic records link to the matching documents view. Publication date filtering excludes missing dates. Filter or library changes clear hidden selections.

Delete, reprocess, review, star changes, content type corrections and topic add/remove share this dialog. The materials toolbar has no separate whole-library review or rebuild shortcuts. All-match scope preserves every current list filter. To reprocess a library, filter by that library; add failed status to target only failures. The intake page retains its explicit pending-review context. Content types come from a single library catalogue. Preview shows eligible, skipped and blocked counts, reasons and samples; a named scope and explicit confirmation precede submission. Delete explains irreversible removal. Reprocess uses current library policy and excludes generated output, whose index maintenance remains separate. Review preserves source text and never performs user rejection deletion.

Accepted submissions show pending/running state, then server terminal counts and paginated item results. No submission toast claims completed success. Errors preserve the dialog and results with refresh recovery. Only failed items can be retried. Pending requests prevent duplicate mutations; generation checks reject stale responses; closing and teardown stop client polling without stopping server work. TDesign owns dialog focus, Escape, selects, date entry and pagination. Both locales and existing semantic theme tokens apply.

Batch submissions retain batch_id in the documents route. The result link and a page reload reopen server status in the root EntryBatchDialog. Review shortcuts hand off to that same root result owner; closing the dialog does not orphan its route.


## Saved entries and background processing

Source: the confirmed library intake rules in ADR 0014 and the request to separate saved state from background work.
`entryProcessingState` supplies saved/background labels to shared status indicators; the owning list and detail views control failed-work retries. A confirmed body save announces Saved (已入库); only accepted/unchanged save results count as success. Drafts and legacy rejected entries do not receive the saved badge.
The entry API's parse_status describes body indexing, and summary_status describes background enrichment. Completed body indexing must not hide pending or failed background work. Both fields participate in polling; a completed body index with failed enrichment retains the saved badge and body access. Retry uses the existing reparse endpoint, disables duplicate activation, updates the owning list/detail from the server response, and preserves readable errors. No retry toast claims processing has completed. Index rebuilding may temporarily affect search availability.

## Weekly content checks

Source: .scratch/weekly-self-check/spec.md (local only). ContentChecksPage under Diagnostics uses shared selects, input, switch, buttons, loading and pagination, the shared tokens and the material detail route. Settings are explicitly saved; no model or schedule is enabled by default. List page and selected run live in URL state. Inline failures remain visible with existing results; only explicit recheck starts another run. Details expose source excerpts as escaped text and navigate through the existing reference/hash validation.


## Swiss UI foundation

Source: the approved Swiss redesign and foundation task (local tracker), with
domain behavior still owned by CONTEXT.md and existing API contracts. This
section applies to newly migrated Swiss pages and the development component
preview. Existing pages keep their canonical TDesign owners above until migrated.

- `frontend/src/components/ui` owns the new reusable controls, table presentation,
  accessible Reka overlays and feedback services. See DESIGN.md for visual rules.
- Field associates one native control with its label, hint and validation error.
  Select uses the native platform popup; Combobox provides authored filtering.
- DataTable receives the current page of rows and controlled checked/focused keys.
  Its select-all action applies to those rows only. Server batch membership,
  pagination and URL filters remain with the owning page and existing APIs.
- Toasts and confirmations share one FeedbackHost under the existing app locale
  provider. Existing MessagePlugin and DialogPlugin call sites remain unchanged.
- Confirmation initially focuses Cancel, prevents duplicate async actions and
  retains a failed action for retry. Dialog and Drawer trap focus and restore
  the opener on close; keyboard behavior belongs to Reka UI.
- New CSS uses rq-prefixed classes; token declarations add no global element
  reset. Global scrollbar rules remain owned by the existing theme.
- Component tests run with a DOM adapter, without starting a browser. Visual
  acceptance remains a separate browser task on `/platform/dev/ui`.


## Swiss materials migration

For `/platform/entries` with the documents tab, DocumentListView owns the list and
table presentation. The list opens a preview on focus or pointer selection; J/K
moves focus and Enter opens the independent detail route. At 900px and below,
the inline preview is hidden and each row offers an explicit Open button.
DataTable owns checked rows, Shift ranges and the single current row. Checked
rows use the soft surface; focus uses yellow. The former grid is removed.

EntryPreview reads the existing detail and spans APIs, ignores old responses,
and polls only while the focused entry is active. Missing traces, unknown counts
and failed reads remain explicit. The list preserves the established URL filters,
server paging and processing polling; switching view does not refetch or clear
filters. LibraryScopeSelect's Swiss variant uses a native multiple select, and
advanced entity/topic filters use native multiple selects. Date filters use native
date inputs with separate start and end labels. Platform popup behavior is accepted.

The materials toolbar and batch bar share EntryBatchDialog. Its existing scope
count, action preview, fingerprint, fixed membership, result pagination and
failed-item retry remain authoritative. Swiss Dialog/Select/Checkbox/Button own
its presentation. The final action uses the shared confirm service; cancelling
retains the preview. The existing ReparseConfirmDialog still owns per-entry
processing configuration. `useLibrary` and the page now report through toast.
Material links now open the independent detail route described below.

The backend does not expose alternate material sort orders, today's intake count,
or a batch Markdown export. The page does not fabricate these capabilities. Its
sort control identifies the supported newest-collected order, and its total is the
filtered result count. Entry identifiers come from actual IDs rather than sample
sequence numbers. New filters invalidate old paging requests and clear checked
rows; failed requests display retry and cannot be confused with an empty library.

## Swiss material detail migration

`/platform/entries/:id` owns full material reading. List/preview, search, follows,
entity sources, related material and command results use this route. Old
`?entry_id=` links preserve their reference and library context through a
compatibility redirect. Browser Back restores the originating URL and filters.

The existing body API supplies the complete saved Markdown and ETag. DocContent
retains a single reading presentation that adds paragraph numbers and
source highlighting after rendering. Stale body hashes show the existing changed
body notice and fall back to paragraph location. Inline images open an accessible
shared Dialog; the saved Markdown can be downloaded locally without a new export
endpoint. The body editor retains draft, conflict and unsaved-leave protection,
with Swiss controls and shared feedback.

Summary, entity and relation data remain server-owned. Relations are obtained
through linked entities' existing relation endpoints, filtered to the current
material and deduplicated by relation ID. Missing details or partial reads show
retry instead of invented rows. Real processing spans supply status, timing and
model information; active work refreshes until terminal status. Responses for a
previous material cannot replace the current route's data.

Star and content-type corrections retain their existing APIs. Reprocessing uses
the stored processing overrides. Confirmed deletion uses the documents endpoint
for personal documents and the entries endpoint for other materials. The shared
confirmation prevents duplicate submissions and retains failures for retry;
pending mutations block route departure. Successful deletion returns to the
material's library. Entity/topic follow controls use the existing follow store.

Reference and processing updates preserve the rendered body when
the Markdown, entry, attachment context and locale are unchanged. Async image
and diagram work is tied to the latest render; the original Markdown renderer
remains the content owner. Hidden tab panels have a shared visibility guard.
The star action keeps its native button enabled while exposing aria-disabled
and blocking duplicate requests, so focus survives a pending star update.


Expanded materials filters must not collapse the results to zero height. The
page provides vertical overflow when necessary, and its results retain their own
scrolling and a 240px minimum height. Narrow tables keep a readable title column
and scroll horizontally inside DataTable. Focused list and table rows use the
shared selection foreground for every descendant, including status labels,
muted text, tags and hovered links; checked rows without focus keep their usual
soft surface and foreground.

## Swiss search recovery and filters

The graph endpoint's HTTP 404 with code 1003 means the entity was not found,
per [the search API contract](docs/search-api.md). Present an empty state with
the selected library name and advice to check the name or change libraries.
Other failures remain errors with retry; editing the entity or library clears
the previous result state.

Entity and topic filters retain arrays of selected IDs. The compact native
select adds one ID at a time, returns to its placeholder after selection, and
selected values have named remove buttons. Empty option lists show localized
explanations. These native selects accept platform-owned popup geometry and
keyboard typeahead, consistent with the Swiss foundation. Reset restores the
existing non-library filter defaults and is hidden when those defaults apply.

## Swiss entity migration

`/platform/entities` owns entity browsing, four detail tabs, follow controls and
explicit same-library merges. Old `entries?tab=entities` bookmarks redirect while
preserving entity, library and reference query context. The temporary
`entries?tab=entities&section=merges` destination retains existing merge review
until the pending page migration. Source links use the independent material
route and retain evidence locations and body hashes.

Entity details, relations and merge suggestions use the existing entity APIs
and library identity rules in CONTEXT.md. Unknown list counts are not fabricated.
Related material types come from the Entry payload. Missing entities have a
specific empty state; request failures offer retry. Switching entity or library
invalidates previous detail and relation responses. Deleting a relation uses the
shared confirmation service and preserves the backend suppression behavior.
Merging uses the shared dialog; failure retains source and target for retry.

After a successful manual entity merge, select the retained entity and replace
the merged ID in the entity page URL. Refresh related lists and follow state
without leaving the user at an empty profile. Graph queries preserve explicit
relation-type selections and the selected relationship limit.


## Swiss topics migration

`/platform/topics` uses the existing topic APIs and the single-library domain
rule in CONTEXT.md. All-library browsing combines one list request per catalog
library; a failed library remains visible as an error with retry while successful
lists stay readable. Library and explicit topic selection are URL-backed.
Creation requires one library, and editing cannot change it. Creating under a
different library filter opens the new topic in its owning library.

Historical assignment polls every two seconds while any visible topic is pending
or running, refreshes counts, and refreshes selected materials when counts change
or assignment finishes. Failed batches retain their reasons, sizes and attempts.
Retry is unavailable while running or cancelled; 404 and conflict responses have
specific recovery messages. Scope changes and teardown invalidate old responses
and stop polling. The shared confirmation retains failed deletion for retry;
success refreshes the list and follow state. Deletion retains the materials,
as described by the existing topic service contract.

Materials use the existing topic and library filters with server pagination at
20 rows. They link to `/platform/entries/:id`, and the existing filtered materials
management link remains available. Association-source filtering is outside the
page's scope; no unavailable-function explanation or reserved control is shown.
Built-in content types display their localized names in the shared Tag; custom
library content types retain their names.
Forms retain drafts on failure, associate validation with fields, and block
duplicate submissions. The shared Dialog, confirm and toast owners handle
modality, feedback and focus; browser verification remains a separate step.

## Swiss diagnostics

Failure filters and paging remain in the URL. Selecting a failure adds its ID
and opens the shared drawer without removing the list. Reading a captured
material never submits or retries ingestion. Closing or replacing a material
invalidates pending reads and releases any image URL. Expired or missing
materials retain explicit errors. Failed saved materials use their existing
processing action and the shared confirmation; retrying all matching failures
uses the existing batch preview, confirmation and progress dialog.

Check records select the first batch on a page unless the URL identifies a
batch. List responses omit reports, so uninspected batches show actual processing
progress instead of an invented problem count. Findings retain their three
categories and exact source locations. Failed checks preserve completed results
and expose a confirmed recheck of unfinished materials. Schedule edits stay in
a draft until saved, use active models, and preserve the backend's Beijing time
semantics. Cancel leaves the saved schedule unchanged. Shared Swiss primitives
own forms, feedback, overlays and keyboard focus on these migrated pages.

## Swiss evaluation and model calls

Evaluation retains set management, all existing question and record filters,
relevance grades, graph parameters, candidate scope, vector selectors, reranking,
similarity collapse, run snapshots and pagination. The backend determines which
confirmed questions run, including each question's retrieval method. Comparison
uses two completed runs of one set and preserves all five deltas, question
revisions and gained or lost hits. Shared dialogs, confirmation and toast services
own editing, deletion and feedback. Failed saves retain the draft and show the
error inside the dialog; changing sets clears old question rows.

Model calls retain UTC inclusive start and exclusive end dates, model, purpose,
material, period and grouping controls. Unknown values remain distinct from
zero and subscription costs; currencies are never added together. Missing trend
days are zero while unknown buckets remain gaps. Only call counts can be split
by success from the existing aggregate API. Clicking a call row or activating it
with the keyboard opens the shared drawer with metadata and the existing complete
request/response viewer. A failed page load cannot leave the previous call rows
under a new page number.

## Swiss command palette

GlobalCommandPalette retains Cmd/Ctrl+K, slash, IME guards, wrapping arrow
navigation and the visible-row Cmd/Ctrl+1 through 9 bindings. Recent targets and
queries share four visible slots, followed by quick navigation and the theme
command. The input exposes the active result through its combobox relationship;
Tab can focus rows and their existing image or update links. Escape closes only
the current overlay and restores the opener.

The existing material search supplies separate material and representative body
snippet groups. Only real snippets receive match labels and source references.
Body snippets require a nonempty chunk ID, a nonnegative chunk index and a source
location. Summary candidates remain material results and open without a reference;
their excerpts and matched image previews remain accessible there.
The existing per-library entity list endpoint supplies name matches, including
entities with no returned materials. Scope changes invalidate prior responses;
partial entity failures retain successful material and entity results with retry.
Material links open `/platform/entries/:id`, snippet links preserve SourceLocation,
and entity links open `/platform/entities` with entity and library context. The
footer and Cmd/Ctrl+Enter preserve query and scope on the full search page.

Recent destinations opened from the palette retain their routes and references
in the existing user/space-scoped browser storage. Clear removes targets and
queries together. Retrieval parameters keep the same API and automatic saving;
the palette's drawer uses native controls and shared feedback. This migration
adds no backend operations or database migration.

Calendar-date values keep the recorded day in every timezone. Timestamp values
continue to display in the viewer’s timezone. `utils/dateTime.ts` owns this distinction.
