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

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Select/Listbox | TDesign Select and TagInput | Existing TDesign installation and locale provider | Filtered, multiple and tag input | Frontend type check and component tests |
| Date | TDesign DatePicker for API Key expiry | ApiIntegrationSettings.vue and active locale | Local minute precision, Unix seconds on submission | API Key expiry tests |
| Form | TDesign inputs and application validation | Existing knowledge base editor and settings forms | Create and edit | Editor validation tests |
| Textarea autosize | SafeTextarea wrapping TDesign Textarea | SafeTextarea.vue and utils/textareaAutosize.ts | Boolean autosize or minRows/maxRows | Actual component mount and height regression tests |
| Scrollbar | Global application theme stylesheet | DESIGN.md and frontend/src/assets/theme/theme.css | Stable gutter for bounded regions | Premium static audit |
| Toast | TDesign MessagePlugin | Existing settings workflows | Success, warning, error and information | Frontend component tests |
| Model tiers | ModelTierEditor and ModelSelector | Model routing API and existing model catalogue | Global default and knowledge base override | Model preview tests and frontend checks |
| Merge suggestions | MergeSuggestions and shared useConfirmDelete | Entity API and ADR 0002 | Pending, accepted and rejected | Backend lifecycle and frontend component tests |
| Model call budgets | ModelCallSummary | Processing span model_calls | Processing details | Budget validation and component tests |
| CRUD | Knowledge base editor, entity API and existing document detail | Entity profiles and knowledge base management | Entity merge refreshes the owning list | Entity lifecycle and editor tests |
| Duplicate intake | KnowledgeDuplicateNotice and useKnowledgeDuplicates | Intake API 409 with optional match_method | Manual, URL and file intake | Actual SFC and hook regression tests |
| Repost sources | RepostSources | Knowledge detail API | Document preview | Shared component and full SFC regression tests |
| Article classification | KnowledgeClassification | Article content_type, processing_mode, structured_summary, entities and topics | Compact list and complete detail | Actual SFC render, link safety and locale tests |

## Flow ledger

| Operation | Pending | Success | Failure recovery |
| --- | --- | --- | --- |
| View entity | Clear the previous article list and show loading | Show type, aliases and articles mentioning the entity in the single knowledge base | Keep selection, show inline error and allow refresh |
| Filter entity list | Debounce search and invalidate older requests | Show the filtered list with explicit load more | Show inline error and allow refresh |
| Merge entity | App-owned dialog names the retained entity and space-wide consequences; disable duplicate requests | Refresh the entity list and announce success | Keep selected target and show inline error |
| Accept merge suggestion | Shared confirmation names both entities and space-wide consequences | Refresh the list, retain filter and announce success | Keep list and show inline error |
| Reject merge suggestion | Disable duplicate requests | Keep entities separate and refresh suggestions | Keep list and show inline error |
| Rebuild entity name embeddings | Disable duplicate starts; poll active runs every five seconds | Show status and processed/total counts | Preserve failure reason and retry controls |
| Collect a duplicate | Existing intake busy controls | Localized notice offers an explicit view-existing-document action | Preserve manual draft and distinguish duplicate from failed batch inputs |

## Dataset and draft state

Entity and suggestion lists use explicit load more with fifty and twenty rows per request. Filter changes invalidate older results. Entity details show the shared identity and articles in the single knowledge base. Workbench filters and selected section are transient within the article view; its main documents/entities tab remains in URL state. Composing Chinese input does not trigger entity search. Clearing search refreshes immediately. Merge target options also use explicit load more and load across the entire space.

Model tiers and article processing configuration save in the knowledge base settings page. The server validates model compatibility. Leaving changed model tiers uses the shared application confirmation dialog. Failed saves preserve drafts and inline errors.

## Layout and interaction

Settings content owns vertical scrolling. Long forms keep their natural height. New controls use TDesign keyboard behavior, visible focus, disabled and busy states, and app-owned dialogs. Native entity selection buttons have visible focus and hover states. Entities and article titles wrap and remain readable in narrow views. Autosizing textareas use SafeTextarea and cancel pending measurements on unmount.

The settings-content container stacks rows at 720px or less. Shared settings-layout.less owns that breakpoint. Existing rules below 640px remain in place.

Document details fill the viewport at widths of 720px or less. Desktop drawer width remains the user's preference. Narrow details hide the desktop resize handle. Long source links wrap; code and tables may keep bounded local scrolling.

Article content types filter the server list and clear hidden selections. Articles saved as a summary and link show their original link, structured summary, entities and topics; their detail view omits body preview and file download. Selecting an article entity opens its profile in the existing entity workbench. Reprocess queues the complete article processing call and polls parse_status while active, replacing the displayed classification, summary and relations on completion. Failed reprocessing keeps the readable server reason visible and allows another attempt. RSS defaults are a classification hint with six content types and an explicit unspecified option.

Source lists show publisher, original URL, publication time or a missing marker, and matching method. URLs use the shared absolute HTTP(S) URL helper; unsafe values remain escaped text. Opening a different document clears old source data and ignores earlier responses.

API Key expiry uses TDesign DatePicker and the active locale. Values retain local date-time at minute precision and convert to Unix seconds on submission. Clearing expiry omits expires_at_unix.

Browser inspection is handled by the user's separate task. This task records static, component, localization, type-check and build evidence without claiming browser verification.

Knowledge search lives at /platform/knowledge-search. TDesign multi-select controls filter content types, entities by ID, topics by ID, and article, user document or report sources. Publication date filters exclude articles without a publication date and show the excluded count. Each result contains one article with at most three matching snippets, summary and source links. Similar reports collapse by default and expand within their leading article; a request switch disables collapse. Search requests invalidate older responses, retain readable errors and expose a retry action. The global search shortcut and API integration Playground share the article search API and supported parameters.

Topic management lives at /platform/topics. Creation and description edits start historical assignment, with processed/total article counts and failed article/batch counts on each topic. Failure reasons remain visible, and retry targets failed batches. Progress polling stops on completion and route teardown; leaving the page does not stop server work. Duplicate-name failures preserve the editor draft. Delete confirmation names the topic and explains removal of article associations. Failed deletions keep the confirmation open.

Article stars wait for server confirmation before updating the card, row or detail. A failed update retains the original state and exposes an inline retryable error. List, search and Playground filters support all, starred and unstarred articles; search persists this state in its URL. Changing a star removes an article from an incompatible active list filter.

The article detail uses KnowledgeArticleJudgments for content type and topic corrections. Full text to summary and link requires a second confirmation stating that body, body index and attachments will be removed. Failure keeps the dialog open. Summary and link to full text keeps the existing article on fetch failure and displays the reason; success reloads body chunks. Corrected types remain visible after reprocessing. Topic corrections save selected IDs and reload current associations. Busy corrections disable reprocessing. Review offers explicit approve, reject, retry and discard actions with shared pending and inline failure states.

The main navigation lists articles, knowledge search, entities, topics, follows and settings. The entity entry opens /platform/knowledge?tab=entities. Articles include the pending review area. Settings provide knowledge base, subscriptions, models, file library status, operation logs, API integration and runtime queues. File library status lives at /platform/settings?section=vault.

Follow controls share server state in entity profiles, topic management and article detail relation labels. Successful mutations update all controls and the sidebar count; failures preserve the prior follow state and show an inline error with retry. Article stars retain their bookmark behavior. Entity merges transfer follows to the retained entity and deduplicate them; topic deletion removes its follow.

The follow feed lives at /platform/follows and shows completed external articles newest collected first, with each article's matching followed entities and topics. It excludes personal documents, reports and both pending review kinds. New markers use the later of feed qualification and association creation, independent of collection time. Historical topic backfill links do not produce new markers or unread counts. Adding a follow does not reset old articles' entrance times. Object filters and the existing similar report switch keep the feed's original new-marker snapshot across refreshes. Each folded article retains its own matches and new marker. Successful first loading records the captured opening time; loading failures do not advance it. The sidebar count tracks distinct unseen articles and refreshes on navigation, visibility and every thirty seconds. Mark all viewed advances the checkpoint and clears new markers. MCP reads share feed data without changing that checkpoint and paginate through a fixed initial window using cursor and next_cursor. External AI owns brief writing and uses write_report to save it.

The operation log lives at /platform/settings?section=operation-logs and lists user actions newest first, with exact local timestamps, displayed timezone, resource ID and before/after values. The previous /platform/operation-logs route redirects to settings and preserves query parameters. Operation type and page are stored in the URL. Filter changes reset the page and invalidate earlier requests. Refresh and failed-read retry preserve readable records. Log entries are read-only.

Pending reviews live at /platform/knowledge?tab=reviews. TDesign tabs divide intake review and processing review. Each tab loads the pending API with its kind and uses twenty records per page. Switching tabs invalidates earlier responses. Refresh errors retain the current list, and mutations refresh the owning page and article list. A removed final record moves to the previous valid page.

KnowledgeReviewActions owns intake and processing review actions in both the pending list and article detail. Missing source links show add URL and review again, collect without URL, and convert to personal document. Collecting without a URL requires confirmation of full preservation and the source unverified marker. Conversion requires confirmation of the mine/ write and intake-record deletion. Full text removal when confirming or correcting a processing suggestion uses the existing article judgment confirmation. Source unverified articles always keep their full text. Failed operations retain the inputs and confirmation with an inline error. The task 10 request and .scratch/kb-reshape/spec.md second-round decisions own these lifecycle rules.

Article cards, rows, detail and the full search page share KnowledgeClassification for the source unverified marker. The global search hook preserves the same field, and ResultItem displays its localized badge. The detail loading hook preserves that field and processing review metadata, resets them on document switches, and ignores older responses. Upload and manual collection explain optional links and the file extraction or pending review outcome. External Agent integration guidance states that model intake review applies to HTTP and MCP collection and that rejected content returns pending_review with a reason.
