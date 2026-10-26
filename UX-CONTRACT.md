# Reliquary UI behavior

## Product context

Reliquary serves a single user collecting source material and maintaining an entity directory. The supported locales are zh-CN and en-US. Existing locale files own product copy. Light and dark themes retain the same hierarchy. New controls target WCAG 2.2 AA.

## Business-context sources

| Scope | Source |
| --- | --- |
| Current entity terminology and component scope | CONTEXT.md and docs/adr/0016-store-and-track-event-layer.md |
| Shared entity maintenance and material attribution | docs/adr/0016-store-and-track-event-layer.md and docs/entity-profiles-and-retrieval.md |
| Features retained during staged implementation | .scratch/kb-reshape/spec.md (local only) and the explicit task 02 request |
| Single-user access | docs/adr/0007-personal-single-user.md |
| Imported personal documents and deletion | docs/adr/0013-user-documents-imported.md |

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Table Selection | DocumentBatchBar and EntryBatchDialog | Server batch preview and fixed membership at submit | Checked loaded rows or all matching results; choose scope before action and preserve list filters | Filter tests, frontend checks and browser batch flow |
| Select/Listbox | TDesign Select and TagInput | Existing TDesign installation and locale provider | Filtered, multiple and tag input | Frontend type check and component tests |
| Date | TDesign DatePicker and DateRangePicker | ApiIntegrationSettings.vue, EntityActivity.vue and active locale | API Key local minute precision; entity fact UTC calendar range | API Key expiry and entity activity tests |
| Form | TDesign inputs and application validation | Existing library editor and settings forms | Create and edit | Editor validation tests |
| Textarea autosize | SafeTextarea wrapping TDesign Textarea | SafeTextarea.vue and utils/textareaAutosize.ts | Boolean autosize or minRows/maxRows | Actual component mount and height regression tests |
| Scrollbar | Global application theme stylesheet | UX-CONTRACT.md and frontend/src/assets/theme/theme.css | Stable gutter for bounded regions | Premium static audit |
| Toast | TDesign MessagePlugin | Existing settings workflows | Success, warning, error and information | Frontend component tests |
| Model tiers | ModelTierEditor and ModelSelector | Model routing API and existing model catalogue | Independent library stages; shared space embedding | Model preview tests and frontend checks |
| Model call budgets | ModelCallSummary | Processing span model_calls | Processing details | Budget validation and component tests |
| CRUD | EntityEditor, DomainEditor, EntryAssociationsEditor, GroupingCorrection, library editor and TDesign dialogs | Entity API and ADR 0016 | Entity management and manual material associations | Route PG tests and frontend checks |
| Repost sources | RepostSources | Entry detail API | Document preview | Shared component and full SFC regression tests |
| Article classification | EntryClassification | Article structured_summary and entities | Compact list and complete detail | Actual SFC render, link safety and locale tests |
| Image preview | ImagePreview wrapping TDesign ImageViewer | SearchImage and picture-preview | Authenticated search Blob and existing picture URL | Real browser Escape, opening focus and trigger focus restoration |

## Flow ledger

| Operation | Pending | Success | Failure recovery |
| --- | --- | --- | --- |
| View entity | Clear the previous material list and show loading | Show type, aliases and materials involving the entity grouped by library | Keep selection, show inline error and allow refresh |
| Filter entity list | Debounce search and invalidate older requests | Show the filtered list with explicit load more | Show inline error and allow refresh |
| Merge entity | App-owned dialog names the retained entity and space-wide consequences; disable duplicate requests | Refresh the entity list and announce success | Keep selected target and show inline error |

## Dataset and draft state

The library catalogue permits an empty list and keeps library creation available. Empty libraries, including historical system libraries, use the existing delete confirmation and server-side emptiness check. Successful deletion refreshes the catalogue; deleting the final library clears its editor. Startup and registration do not recreate deleted libraries. The four template behaviors remain available when creating a library. Source: ADR 0014, revised 2026-10-21.


Entity lists use explicit load more with fifty rows per request. Filter changes invalidate older results. Entity details show the shared identity and materials across libraries. Entity directory search and type filters are transient within the material view; its main documents/entities tab remains in URL state. Every article list filter persists in the URL: library scope, starred, keyword, entity_ids, domain_ids, published_from and published_to. Default values are omitted. Refresh and history navigation restore the controls and results together, retaining unrelated query parameters. The source category reuses the search source values and labels. Filters combine in both cards and rows. The filter panel uses aligned, wrapping controls; review and rebuild actions remain separate from the filter grid. Switching filters resets paging and clears hidden selections. Composing Chinese input does not trigger entity search. Clearing search refreshes immediately. Merge target options also use explicit load more and load across the entire space.

Model tiers and article processing configuration save in the library settings page. The server validates model compatibility. Leaving changed model tiers uses the shared application confirmation dialog. Failed saves preserve drafts and inline errors.

PromptSettings at `/platform/settings?section=prompts` manages material understanding, long-material consolidation, fact grouping, story consolidation, output correction and evaluation prompts for the selected library. The scope selector defaults to the first library; shared global prompts cover cross-library evaluation and model checks and remain available with no libraries. The explicit scoped-prompt request and `/api/v1/system/prompts` contract define this flow. File templates provide defaults; saved database values take priority and apply to subsequent model calls without restarting or reprocessing existing materials. The page uses the existing TDesign controls, SafeTextarea and settings draft confirmation. Switching scopes, switching templates or leaving with unsaved edits requires a discard decision; failed saves keep the draft and current effective content. Superseded requests cannot overwrite another scope. Restoring a default confirms the scope, prompt and discarded edits, then reloads server content; save and reset feedback names the affected scope. Prompt settings appear only in this editor, not in the generic runtime-settings table. Both supported locales use the same compact list/editor layout and stack in narrow containers.

## Layout and interaction

Settings content owns vertical scrolling. Long forms keep their natural height. New controls use TDesign keyboard behavior, visible focus, disabled and busy states, and app-owned dialogs. Native entity selection buttons have visible focus and hover states. Entities and article titles wrap and remain readable in narrow views. Autosizing textareas use SafeTextarea and cancel pending measurements on unmount.

The settings-content container stacks rows at 720px or less. Shared settings-layout.less owns that breakpoint. Existing rules below 640px remain in place.

Document details fill the viewport at widths of 720px or less. Desktop drawer width remains the user's preference. Narrow details hide the desktop resize handle. Long source links wrap; code and tables may keep bounded local scrolling.

All materials show the complete Markdown body alongside the original link, structured summary and entities. Selecting an entity opens its details in the existing workbench. Reprocess queues the existing processing call and retains failure feedback and retry.

Source lists show publisher, original URL, publication time or a missing marker, and matching method. URLs use the shared absolute HTTP(S) URL helper; unsafe values remain escaped text. Opening a different document clears old source data and ignores earlier responses.

API Key expiry uses TDesign DatePicker and the active locale. Values retain local date-time at minute precision and convert to Unix seconds on submission. Clearing expiry omits expires_at_unix.

Browser inspection is handled by the user's separate task. This task records static, component, localization, type-check and build evidence without claiming browser verification.

Knowledge search lives at /platform/search. Shared TDesign controls select the library scope, publication dates and star state. Results show each entry independently with at most three matching snippets, summary and source links. Publication date filters exclude undated entries and display the excluded count. Requests invalidate older responses, retain readable errors and expose retry. The global search shortcut and API integration Playground share these controls and the search API.


Article stars wait for server confirmation before updating the card, row or detail. A failed update retains the original state and exposes an inline retryable error. List, search and Playground filters support all, starred and unstarred articles; search persists this state in its URL. Changing a star removes an article from an incompatible active list filter.

The article detail uses EntryArticleJudgments for the shared star control. The original body and images remain readable after processing failure.

The main navigation groups existing entries into Materials, Needs attention, Diagnostics and Configuration. Needs attention opens /platform/entries?tab=reviews and retains only intake review and missing images. Diagnostics groups the existing evaluation and model-call pages. Existing contextual entries remain available. The entity entry opens /platform/entries?tab=entities. Settings retain library, models, file library status, operation logs, API integration and runtime queues. File library status lives at /platform/settings?section=vault.

Follow controls share server state on entity pages, story lists, story details and follow-feed cards. Follow and unfollow are idempotent and record actual changes in the operation log. Entity and story merges transfer follows to the retained object, deduplicate them and keep the earliest follow time. Old merged IDs resolve to the retained target.

The follow feed at /platform/follows shows event cards for followed stories or stories with a fact whose subject or object is a followed entity. Material entity tags alone do not trigger it. Each card highlights the latest qualifying fact with its source quotations, the latest three eligible facts, matching follows and a link to the story timeline. Only source material from feed-enabled libraries is included; mentions do not qualify. Ordinary facts without live sources stay hidden, while manually corrected facts without sources remain visible with a No source label. These unsourced corrections have no library material to attribute; a scope containing no enabled libraries returns no cards.

Cards sort by their latest eligible fact creation time, then story ID. Additional sources and follow creation do not advance that time. Library and followed-target filters, page size, cursor pagination, loading, retry and empty states remain available. The feed has no new markers, viewing checkpoints, mark-as-seen action or sidebar unread count. HTTP and MCP return the same story, trigger_fact, recent_facts and matched_follows card data. Cursors retain the first request's fact cutoff and filters; live source withdrawal and current library settings still apply when continuing. External AI reads source material and writes its own summaries through ingest.

The operation log lives at /platform/settings?section=operation-logs and lists user actions newest first, with exact local timestamps, displayed timezone, resource ID and before/after values. The previous /platform/operation-logs route redirects to settings and preserves query parameters. Operation type and page are stored in the URL. Filter changes reset the page and invalidate earlier requests. Refresh and failed-read retry preserve readable records. Log entries are read-only.

Embedding model settings persist supports_images and the Jina multimodal format and send them through the model connection test. The library model tab allows changing the embedding model and exposes all-vector rebuilding. Its progress is loaded from the API and polled every two seconds while active. Pause, resume and failed-item retry preserve successful checkpoints; errors retain the latest readable progress. Changing model identity requires a new run. Retrieval uses only the current model's vectors and keeps keyword search available while replacements are built. SearchImage loads image snippets in both full and global search through the existing authenticated attachment request, cancels stale requests and releases Blob URLs on replacement or unmount.

Pending reviews live at /platform/entries?tab=reviews and contain legacy materials awaiting a manual decision. New intake does not enter this queue. Missing images do not create a workflow, badge or filter. Reprocessing uses the shared bulk flow for selected rows or matching materials, including AI output. Refresh failures retain the list; resolving its last record returns to the previous valid page.

ReviewActions owns manual collection and rejection in the pending list and article detail. Source links remain optional: users can add a URL and collect, or confirm collection without one while retaining the source-unverified marker. Rejection still requires confirmation. Failures retain inputs and the open confirmation. The interface offers no library-wide or batch review action; the removed HTTP route and batch request validation prevent new review jobs. Historical jobs and results remain supported.

Article cards, rows, detail and the full search page share EntryClassification for the source unverified marker. The global search hook preserves the same field, and ResultItem displays its localized badge. The detail loading hook preserves that field , resets them on document switches, and ignores older responses. New content arrives only from external tools through HTTP and MCP ingest interfaces. The article page has no URL import, file upload or manual creation controls. Archived entries offer explicit body editing in the existing detail drawer. EntryContentEditor owns the complete Markdown draft, preview switch, save feedback and unsaved-change confirmation; DocContent owns rendering, attachment access and processing status. The editor submits the read body's hash and retains the draft on failure or conflict, with copy/reload recovery. Entry ID, source and title remain unchanged; saved edits confirm storage immediately and run indexing and enrichment in the background, without body history or persistent drafts. Source: ADR 0014, docs/ingest-api.md and the explicit web-editing request. Legacy manual articles and their drafts remain readable in the detail drawer. ReparseConfirmDialog owns existing article reprocessing configuration. Legacy RSS and web channels retain their display labels. Remote image references display a localized missing-file link without loading or probing the source. Supplied attachments, inline images and same-origin images remain viewable. External Agent integration guidance describes technical admission checks, immediate storage and independent background processing for HTTP and MCP collection.

## 界面命名与样式约定

Reliquary 保留现有页面结构、色彩、字体和交互。产品显示名称为 Reliquary；Go/TypeScript 导出符号按语言惯例使用 Reliquary。登录页与侧栏共用 frontend/src/assets/img/reliquary.svg。

唯一库的根面包屑使用 library.title，中文显示“库”，英文显示“Library”，不随数据库中的库名称变化。文章、待处理、实体及设置背景共用此名称。

实体页面维护机构和人物名录。实体详情显示规范名、类型、别名以及跨库涉及该实体的资料，资料可通过原有详情入口打开。新增、编辑、删除和合并沿用 TDesign 组件与中英文文案，不再提供待合并建议和名称向量重建。

运行时样式以 frontend/src/assets/theme/theme.css 为唯一色彩与圆角来源，经 frontend/src/main.ts 加载后供 TDesign 组件及 var(--td-*) 使用。字体由 frontend/src/composables/useFont.ts 管理，保留既有中文与英文选择。新增区域不另建色彩、字体或阴影体系。滚动条使用全局主题变量，所有新增溢出区域自动继承。

实体列表使用显式加载更多，实体详情保持自然高度。窄屏把实体列表与档案改为上下排列，长实体名、别名和文章标题换行。实体合并前明确保留实体及资料关联、关注的转移；删除前确认所删实体与关联范围。

模型档位在设置中心的库设置中配置，使用 ModelTierEditor 与原有 ModelSelector。库目录按选中库展示统一的只读入库规则，并编辑适用模型、分块参数；生成型也可配置这些处理能力。审核标准、来源门槛和按类型抽图开关不再提供编辑。共享嵌入页签仅配置空间向量模型。每一行显示用途、选择和生效来源，不暗示其他库提供默认值。切库和离开前沿用未保存确认，失败保留草稿。父子分块开关按模板锁定。待审区明确标为历史资料的人工处置入口。资料和实体页面使用 /platform/entries；内容由外部工具通过收录接口送入。模型调用继续使用共享 ModelCallSummary 表格。

主导航按资料管理、待处理、辅助诊断、系统配置分组。待处理直达原待审区，只保留收录待审；实体入口打开现有实体详情页签。设置中心提供库、模型、文件库状态、操作日志、API 集成和运行队列入口，沿用原有桌面外观。较窄的桌面窗口先减少内容左右留白，再收窄侧栏；内容使用命名容器 settings-content，实际可用宽度不超过 720px 时，标签与控件上下排列。断点与行布局由 frontend/src/assets/settings-layout.less 统一维护，控件可收缩，表格只在自身区域横向滚动。既有 640px 以下规则继续保留。

自动增高文本框共用 SafeTextarea，保留 TDesign 外观与输入交互，项目负责可取消的安全高度测量。API Key 有效期使用 TDesign 日期时间选择器与全局中英文语言配置，保持原本本地时间、分钟精度与 Unix 秒提交方式。

Embedding 模型编辑提供支持图片开关和 Jina 请求格式，模型卡显示图片能力。库模型页签使用 EmbeddingRebuild 展示全部向量重建的总数、完成数、失败数及暂停、继续、失败重试动作。检索结果与全局快捷搜索共用 SearchImage，沿用附件鉴权与主题变量，缩略图保留固定区域并提供加载和失败状态。大图预览共用 ImagePreview 与 TDesign ImageViewer，打开移入焦点，Esc、关闭按钮和遮罩关闭后返回触发元素。新增设置沿用现有 TDesign 行布局，不另建外观体系。

文章列表和详情共用 EntryClassification 展示处理方式、概述、领域和涉及的实体。列表用紧凑字号显示一句话概述，卡片随内容自然增高。实体是可聚焦的按钮。所有文章显示可换行的安全原文链接和已保存的 Markdown 正文。

知识检索页使用 /platform/search。查询框配合库、发布时间和星标筛选；资料逐篇显示。每篇文章只展示一条主结果，最多三个命中片段，文章提供原文入口。缺失发布时间的排除数量在结果区说明。全局搜索继续作为快捷入口，API 集成页的 Playground 使用相同参数。筛选与结果沿用现有主题变量，窄屏自然换行。

操作日志在 /platform/settings?section=operation-logs，原 /platform/operation-logs 入口重定向到设置并保留筛选参数。日志按时间倒序显示修改前后值，并说明本地时区。操作日志沿用既有 TDesign 控件、主题变量、自然高度记录和窄屏换行。

文章卡片、列表行与详情共用 EntryStarToggle。详情通过 EntryArticleJudgments 修改星标。文章列表、检索与 API Playground 的星标筛选都提供全部、已星标、未星标三个状态。

Personal documents arrive through ingest with kind=mine. Pending intake and missing-image lists show the personal document source badge. Missing URLs do not expose the article missing-link actions or prevent approval. Cards and rows use personal document wording that names the file, attachments and index and explains reimport for changes. The detail header uses PersonalDocumentDelete with the shared DocumentDeleteConfirm. Article and personal document card and row menus close before opening that shared confirmation outside the Popup. The modal attaches to body, shows its complete content and blocks underlying entry clicks with its overlay. During deletion, both the confirmation and the enclosing detail drawer reject overlay, Escape and close-button dismissal; their callbacks also reject closing. Duplicate submission is disabled. Failures retain the open dialog, readable error and retry action. Closing becomes available when the request finishes. Successful deletion closes the confirmation and refreshes the article list while preserving filters; detail deletion also closes the drawer and refreshes pending lists. Personal document details use the shared body editor and background processing status, including retry after failure. File library status describes imports and does not show a scan timestamp for mine/.

## Bulk material management

The materials toolbar always offers the shared EntryBatchDialog, including when no loaded row is checked. DocumentBatchBar opens the same dialog for checked rows. Scope selection is step one; action selection and preview are step two. Both steps retain a scope summary of the captured filters and the exact server batch count, including unloaded and uncollapsed materials. Checked scopes distinguish available and unavailable IDs and never expand to all matching filters. Count failures stay visible with retry; loading or failed counts block advancement. Scope count requests are independent of action previews and ignore stale responses. Returning to scope preserves this summary but invalidates the action preview and confirmation. Checked rows and all matching server results are separate scopes. All-match selection includes unloaded results and fixes membership at confirmation submission. Entity IDs, topic IDs and publication dates join existing URL-backed document filters. Entity details and topic records link to the matching documents view. Publication date filtering excludes missing dates. Filter or library changes clear hidden selections.

Delete, reprocess, review, star changes, content type corrections and topic add/remove share this dialog. The materials toolbar has no separate whole-library review or rebuild shortcuts. All-match scope preserves every current list filter. To reprocess a library, filter by that library; add failed status to target only failures. The intake page retains its explicit pending-review context. Content types come from a single library catalogue. Preview shows eligible, skipped and blocked counts, reasons and samples; a named scope and explicit confirmation precede submission. Delete explains irreversible removal. Reprocess uses current library policy and excludes generated output, whose index maintenance remains separate. Review preserves source text and never performs user rejection deletion.

Accepted submissions show pending/running state, then server terminal counts and paginated item results. No submission toast claims completed success. Errors preserve the dialog and results with refresh recovery. Only failed items can be retried. Pending requests prevent duplicate mutations; generation checks reject stale responses; closing and teardown stop client polling without stopping server work. TDesign owns dialog focus, Escape, selects, date entry and pagination. Both locales and existing semantic theme tokens apply.

Batch submissions retain batch_id in the documents route. The result link and a page reload reopen server status in the root EntryBatchDialog. Review shortcuts hand off to that same root result owner; closing the dialog does not orphan its route.


## Saved entries and background processing

Source: the confirmed library intake rules in ADR 0014 and the request to separate saved state from background work.
EntryProcessingStatus owns saved/background labels and failed-work retry across document cards, rows and details. A confirmed body save announces Saved (已入库); only accepted/unchanged save results count as success. Drafts and legacy rejected entries do not receive the saved badge.
The entry API's parse_status describes body indexing, and summary_status describes background enrichment. Completed body indexing must not hide pending or failed background work. Both fields participate in polling; a completed body index with failed enrichment retains the saved badge and body access. Retry uses the existing reparse endpoint, disables duplicate activation, updates the owning list/detail from the server response, and preserves readable errors. No retry toast claims processing has completed. Index rebuilding may temporarily affect search availability.

## Weekly content checks

Source: .scratch/weekly-self-check/spec.md (local only). ContentChecksPage under Diagnostics reuses TDesign selects, input, switch, buttons, loading and pagination, the shared theme and the existing entry reference route. Settings are explicitly saved; no model or schedule is enabled by default. List page and selected run live in URL state. Inline failures remain visible with existing results; only explicit recheck starts another run. Details expose source excerpts as escaped text and navigate through the existing reference/hash validation.


Entity maintenance follows ADR 0016. The Entities page edits institutions and people with canonical names and explicit aliases, using the existing TDesign dialogs and MessagePlugin. Canonical name, type and aliases use native input, select and textarea controls with labels bound to the actual controls. Deletion names the entity and explains removal of links and follows while retaining material content. Merge moves links and follows to the retained entity. Removed merge suggestions and entity vector rebuilding have no navigation or actions.

Material details allow an explicit choice of zero to three existing entities and display the manual-edit marker, including an empty choice. Fresh detail loads restore this marker; closing or switching details resets it. Saving associations updates only entity fields, preserving enriched library names and formatted titles in the current detail and list cards. Material-list entity filters persist in entity_ids and participate in batch selection. Directory loading, errors, retries and paging use existing compact controls. These changes retain DESIGN.md tokens and the zh-CN/en-US locale policy.


## Domains and mixed material filters

Source: CONTEXT.md and docs/adr/0016-store-and-track-event-layer.md. `/platform/entries?tab=domains` owns the shared domain list, also reachable from material navigation. DomainEditor edits the name and one-sentence definition with real label associations, application validation, an IME guard and useDialogDraft protection. Save failures preserve the draft. Deleting a domain names it and confirms removal from every material; the material body remains. Startup imports Agent and LLM only once and respects later edits and deletions.

Material details show domains through EntryClassification. EntryDomainsEditor and EntryEntitiesEditor use EntryAssociationsEditor for 0–3 selections, a manual marker, busy state, inline errors, retry and draft protection. Empty selections are also manual choices. Saved association events update only the association fields, preserving the current title and library label.

The material list has one grouped TDesign Select for entities and domains. It uses distinct entity and domain values and persists entity_ids and domain_ids in the URL. Entity conditions intersect with domain conditions; options within either kind are alternatives, matching the existing search contract. Publication dates, library scope and stars still compose with those conditions. Bulk scope summaries include both kinds and the server counts the same filters. Returning from maintenance refreshes available options. Domain reclassification opens EntryBatchDialog with all existing materials across all libraries. The preview fixes membership on submission and names skipped manual domain choices. The background task changes only domains, exposes per-item results and retries, and preserves titles, overviews, entities, content and indexes. Event filters remain a later task.


## Stories and fact timelines

Revision and deletion withdraw a material's source and mention links. StoriesPage omits hidden automatic facts and stories without visible facts. Manually corrected facts remain in the timeline and show the localized No source label in the existing secondary text style when `no_source` is true. Recovered evidence retains the fact and story links and does not count as a new fact. Source: ADR 0016 and docs/ingest-api.md.

Source: CONTEXT.md and ADR 0016. `/platform/stories` lists stories by the latest new fact, with server pagination persisted in `page`. The list shares EntityDomainSelect with the material list: one grouped multi-select mixes entities and domains, persists `entity_ids` and `domain_ids` in the URL, and resets to the first page when selections change. Entity matches use either endpoint of visible facts; domain matches use the union of source material domains, excluding mentions. Conditions intersect at the story level, with choices within each group matching any. Hidden facts cannot supply matches; manually corrected facts without sources still supply endpoint matches but no domains. Options support loading more entities, retry and selected ID fallbacks; no matches has its own localized empty state. `/platform/stories/:storyId` preserves the list query on return and shows the chronological fact timeline, source material links and verbatim quotations. Materials open through the existing `entry_id` route. Sidebar navigation remains active on both story routes.

StoriesPage reuses TDesign loading, empty states, retry buttons and pagination. Requests ignore superseded responses and unmounts; refresh errors retain readable results, while changing stories clears the previous detail. Missing stories show a localized error and a route back to the list. Titles and all state labels follow the active locale. Occurrence dates containing only YYYY-MM-DD display that calendar date verbatim, without timezone conversion or an invented time. Occurrence timestamps and first/latest new-fact timestamps retain the existing localized browser-timezone display. Fact frames and quotations are escaped text, wrap at narrow widths and use existing DESIGN.md tokens. The story detail lists mentioning materials separately from the source timeline, deduplicated per material. These links use the existing entry route and the same loading, refresh, error and empty-state owner. Mentions are discussion rather than evidence and never change the latest new-fact time. Related story links expand only one level.

EntityCandidates extends the existing entity workbench with occurrence-ranked pending and ignored names. Each full-width candidate row is a keyboard-operable disclosure with an explicit Review action and a visible selected state. Its source evidence and review controls expand directly inside that row; switching rows or collapsing a row invalidates earlier detail requests. All material types can contribute candidate names with literal source evidence. Material occurrences show their title, an entry_id link and the quoted evidence; fact occurrences retain their event link and endpoint frame. TDesign Select owns status and target selection; both target choices and the combined occurrence list use explicit load more. Review actions wait for server confirmation, prevent duplicate submission, retain the selected target on failure and show inline errors. Approval and alias review attach recorded material sources while preserving manual choices and the three-entity limit. Ignoring preserves evidence and permits later approval through the ignored list. Candidate review uses the same direct-save pattern as entity editing. EntityEditor owns manual alias additions, which backfill only exact, unlinked historical endpoints. Both locales use the same labels, semantic tokens, status feedback and narrow-screen stacking as the existing entity workflow. Candidate status is transient within the entity tab, following the directory filter policy above. Sources: CONTEXT.md, ADR 0016 and docs/entity-profiles-and-retrieval.md.


GroupingCorrection owns the four manual grouping operations in the story detail timeline. It reuses TDesign Dialog, Select, Input, Button and MessagePlugin. Destination stories load in explicit pages of fifty; fact merge first selects a story and then a fact to retain. The dialog names the source item, shows the selected destination, and explains evidence transfer and the absence of automatic merge undo. Busy saves prevent duplicate submits and dismissal. Failures preserve selections with an inline retryable error; successful saves navigate to the retained story and fact. Manually corrected labels use the existing secondary text treatment. Old story IDs and fact anchors resolve to their retained targets. Source: ADR 0016 and docs/entity-profiles-and-retrieval.md.


ModalDialog wraps the installed TDesign Dialog for GroupingCorrection and EntityEditor, including the entity discard confirmation. It first mounts the TDesign instance hidden, then opens it so initial visibility does not skip the library lifecycle. The shared useModalFocus owner supplies dialog semantics, focus entry and restoration, forward and reverse Tab containment, and Escape handling. Only the top modal handles keyboard and focus events. A busy modal cannot close; a child select can consume Escape before the dialog. EntityEditor retains its existing unsaved-change confirmation. Other dialog workflows are outside this change.

Story fact anchors wait for timeline rendering and scroll the stories-page container using the target's local offset. Canonical and rewritten merged IDs use the same behavior; superseded loads cannot move the scroll position. GroupingCorrection bounds both destination popups to their trigger width and the viewport, attaches them within the modal outside its scrolling content, and wraps long option labels. Native select and textarea behavior in EntityEditor retain their existing ownership for this keyboard repair. Browser verification remains a separate human check.


## Entity activity

Source: CONTEXT.md, ADR 0016 and docs/entity-profiles-and-retrieval.md. EntityActivity extends the existing entity detail in `/platform/entries?tab=entities`. Material groups retain their owner in EntryWorkbench. EntityActivity owns the one-hop graph and the chronological paginated fact sidebar, using TDesign DateRangePicker, Select, Loading and Pagination. Native node and directed-edge buttons expose focus, hover and pressed states. Center selection and graph filters are transient within the existing entity workbench, following its directory detail state; the existing `entity_id` query remains the external entry point.

Selecting an edge filters by its other entity and direction. Selecting a neighbor changes the center, retains the date range and clears counterpart and direction filters so the sidebar shows all its visible facts, including single-ended facts. Selecting the center or All facts clears those filters and returns to page one. Counterpart changes and date changes reset paging. The date range applies to graph and timeline together, including both UTC calendar days; unknown occurrence time falls back to fact arrival time, as explained next to the control. Material groups remain independent of the fact period.

Entity and story timelines share the backend visibility and source hydration owners. Hidden or merged facts never enter activity queries; manually corrected facts without sources remain visible with the shared No source label. Graph edges require two current entity endpoints and merge only in the same direction. A graph with no edges keeps its center and the fact sidebar available. Graph and timeline have independent loading, empty, error and retry states. Refresh failure retains readable results; changing a center or filter clears incompatible results and invalidates earlier responses. Both locales and themes retain DESIGN.md tokens. Real popup, keyboard, responsive and theme inspection remains a separate human browser check.

## 本机设置入口

来源：ADR 0015 与 2026-10-26 的登录及辅助入口清理需求。侧栏设置按钮保留通用、工作区、模型、全部设置和系统管理入口，不显示头像、账号资料或项目帮助外链。按钮支持键盘展开，Escape 关闭并返回触发按钮；窄屏沿用现有导航抽屉。工作区直接通过 `/api/v1/space` 加载，暂时断网保留已加载状态和可重试提示。主题、字体和最近搜索保存为本机偏好，升级沿用旧值。
