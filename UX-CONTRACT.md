# Reliquary UI behavior

## Product context

Reliquary serves a single user collecting source material and maintaining entity profiles. The supported locales are zh-CN and en-US. Existing locale files own product copy. Light and dark themes retain the same hierarchy. New controls target WCAG 2.2 AA.

## Business-context sources

| Scope | Source |
| --- | --- |
| Current entity terminology and component scope | CONTEXT.md and docs/adr/0009-kb-collect-store-retrieve.md |
| Shared entity identity and article attribution | docs/adr/0002-global-entity-registry.md and the task 01 entity API contract |
| Features retained during staged implementation | .scratch/kb-reshape/spec.md and the explicit task 01 request |
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

## Flow ledger

| Operation | Pending | Success | Failure recovery |
| --- | --- | --- | --- |
| View entity | Clear the previous article list and show loading | Show type, aliases and articles mentioning the entity from all knowledge bases | Keep selection, show inline error and allow refresh |
| Filter entity list | Debounce search and invalidate older requests | Show the filtered list with explicit load more | Show inline error and allow refresh |
| Merge entity | App-owned dialog names the retained entity and space-wide consequences; disable duplicate requests | Refresh the entity list and announce success | Keep selected target and show inline error |
| Accept merge suggestion | Shared confirmation names both entities and space-wide consequences | Refresh the list, retain filter and announce success | Keep list and show inline error |
| Reject merge suggestion | Disable duplicate requests | Keep entities separate and refresh suggestions | Keep list and show inline error |
| Rebuild entity name embeddings | Disable duplicate starts; poll active runs every five seconds | Show status and processed/total counts | Preserve failure reason and retry controls |
| Collect a duplicate | Existing intake busy controls | Localized notice offers an explicit view-existing-document action | Preserve manual draft and distinguish duplicate from failed batch inputs |

## Dataset and draft state

Entity and suggestion lists use explicit load more with fifty and twenty rows per request. Scope changes invalidate older results. Entity details show the shared identity and articles across all knowledge bases. Workbench scope, filters and selected section are transient within the owning knowledge base view; its main documents/entities tab remains in URL state. Composing Chinese input does not trigger entity search. Clearing search refreshes immediately. Merge target options also use explicit load more and load across the entire space.

Global model tier saves stay in space settings. Knowledge base overrides save with the existing edit draft. Clearing an override restores inheritance. The server validates model compatibility. Leaving changed model tiers uses the shared application confirmation dialog. Failed saves preserve drafts and inline errors.

## Layout and interaction

Settings content owns vertical scrolling. Long forms keep their natural height. New controls use TDesign keyboard behavior, visible focus, disabled and busy states, and app-owned dialogs. Native entity selection buttons have visible focus and hover states. Entities and article titles wrap and remain readable in narrow views. Autosizing textareas use SafeTextarea and cancel pending measurements on unmount.

The settings-content container stacks rows at 720px or less. Shared settings-layout.less owns that breakpoint. Existing rules below 640px remain in place.

Document details fill the viewport at widths of 720px or less. Desktop drawer width remains the user's preference. Narrow details hide the desktop resize handle. Long source links wrap; code and tables may keep bounded local scrolling. Anomaly links and drawers remain in this staged task.

Source lists show publisher, original URL, publication time or a missing marker, and matching method. URLs use the shared absolute HTTP(S) URL helper; unsafe values remain escaped text. Opening a different document clears old source data and ignores earlier responses.

API Key expiry uses TDesign DatePicker and the active locale. Values retain local date-time at minute precision and convert to Unix seconds on submission. Clearing expiry omits expires_at_unix.

Browser inspection is handled by the user's separate task. This task records static, component, localization, type-check and build evidence without claiming browser verification.
