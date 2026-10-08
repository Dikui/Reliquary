# Reliquary UI behavior

## Product context

Reliquary serves a small team that collects source material and checks entities, facts, events and evidence. The supported locales are zh-CN and en-US; existing locale files own product copy. Light and dark themes retain the same hierarchy. The accessibility target for new controls is WCAG 2.2 AA.

## Business-context sources

| Scope | Source |
| --- | --- |
| Team visibility and roles | CONTEXT.md and docs/adr/0003-single-team-space.md |
| Global entity identity and source attribution | docs/adr/0002-global-entity-registry.md |
| Domain templates and field consolidation | docs/adr/0004-domain-templates-open-fields.md |
| Model routing, segmented extraction and uncertain identity | .scratch/architecture-reshape/spec.md, phase P3, sections 6.4 to 6.6 |
| Built in domains and deletion restrictions | .scratch/architecture-reshape/spec.md, phase P2 |

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Select/Listbox | TDesign Select and TagInput | Existing TDesign installation and locale provider | Filtered, multiple and tag input | Frontend type check; browser verification required when available |
| Date | Existing typed storage with native date input | KnowledgeWorkbench.vue | Existing native date picker accepts platform locale and geometry | Frontend type check |
| Form | TDesign inputs and application validation | DomainFieldEditor.vue and domainTemplate.ts | Domain creation, template editing, field consolidation | domainTemplate.test.ts |
| Scrollbar | Global application theme stylesheet | DESIGN.md and frontend/src/assets/theme/theme.css | Stable gutter for bounded regions | Premium static audit; browser verification required when available |
| Toast | TDesign MessagePlugin | Existing settings workflows | Success, warning, error and information | Frontend type check |
| Model tiers | ModelTierEditor and ModelSelector | Model routing API and existing model catalogue | Team default, domain override and knowledge base override | Model preview tests; frontend checks; browser verification |
| Merge suggestions | MergeSuggestions and shared useConfirmDelete | Global entity merge API and ADR 0002 | Pending, accepted and rejected | Backend lifecycle tests; browser verification |
| Model call budgets | ModelCallSummary | Evidence model_calls and processing spans | Evidence details and processing details | Span budget validation test; browser verification |
| CRUD | Knowledge domain API and settings view | DomainSettings.vue | Return to list on domain save; stay on consolidation after mapping | Template validation tests and backend domain tests |
| Duplicate intake | KnowledgeDuplicateNotice and useKnowledgeDuplicates | Message intake API 409 with optional match_method | Manual, URL and file intake | Actual SFC and hook regression tests |
| Repost sources | RepostSources | Knowledge detail and context APIs | Message preview, evidence and event heat details | Shared component and full SFC regression tests |
| Research report body | ReportMarkdown with existing chat Markdown renderer and sanitizer | Workflow report Markdown and citations | Report body, Markdown and JSON downloads | Markdown nodes, evidence navigation and download request tests |
| Wiki directory | WikiReadingLayout with TDesign Drawer | WikiBrowser page and index selection | Persistent desktop directory; closed mobile drawer below 768px | WikiReadingLayout and WikiBrowser component tests |

## Flow ledger

| Operation | Pending | Success | Failure recovery |
| --- | --- | --- | --- |
| Create or edit domain | Form controls and save action disabled | Return to domain list with success toast | Keep the draft and show inline error |
| Delete domain | Shared useConfirmDelete dialog and busy action | Refresh and clamp the current list page | Keep list visible and show error; server prevents referenced domain deletion |
| Map or create a template field | Dialog controls disabled | Refresh selected domain and open-field statistics, remain in consolidation | Keep dialog values and show inline error |
| Query context | Existing loading controls | Keep query and show source-labelled results | Preserve query, show error and allow retry |
| View entity | Clear previous detail and show loading indicator | Show facts and events from all team knowledge bases | Preserve selection and show error |
| Collect a duplicate | Existing intake busy controls | Keep a localized notice with an explicit view-existing-message action | Preserve the manual draft; URL and file batches count duplicates separately from failed inputs |

Built in domain deletion stays disabled with an explanation. Viewers can browse domains; only owner and admin roles see mutations. The server remains authoritative for permissions.

## Dataset and draft state

The settings dialog uses transient local search, tab and pagination state. Domain and open-field tables show ten rows per page. Context lists use explicit load more. Scope changes reset scoped results and invalidate requests; an old response cannot replace results from a later scope. Domain template editing uses an independent copy of saved data. Leaving a dirty template requires the existing application confirmation dialog. Field consolidation is available after template changes are saved or cancelled.

## Layout and interaction

The settings content owns vertical scrolling. Long template forms keep their natural height. New controls use TDesign keyboard behavior, visible focus, disabled and busy states, and application-owned dialogs. New icon-only actions have localized labels. Chinese input composition does not submit context queries. Textareas resize through their existing autosize owner and cannot be manually resized.

Wiki reading keeps the existing desktop columns. Below 768px, the directory starts closed and opens in the existing TDesign modal drawer; selecting a page or the index closes it. The reader uses the full content width and wraps header actions. Source modification issues open the affected page and focus its source notice. Ignoring an issue or source notice refreshes the page marker, page and global issue lists, and pending count. Successful queued regeneration refreshes the same state. These behaviors follow `.scratch/knowledge-data-layering/issues/05-wiki-stale-after-chunk-edit.md` and its browser fix request.

Browser checks cover the changed flows, both locales and themes, keyboard input, narrow viewports, open select popups and failure recovery when browser tooling is available. Static checks and unit tests alone do not prove these runtime properties.

## P3 configuration and review

Team model tier saves stay in space settings. Domain and knowledge base overrides remain part of their existing edit drafts and save with those drafts. Every tier displays its effective model and source, and clearing an override restores inheritance. The server validates model compatibility. Team and domain edits require administrator access; knowledge base edits follow the existing editor permission. Pending requests disable the relevant controls, and failures preserve the draft with an inline error and retry. Leaving changed team or domain model tiers through settings navigation, adding a model or closing settings uses the shared application confirmation dialog.

Merge suggestions use explicit load more with twenty rows per request and transient local status filters, consistent with other context lists. Scope and status changes invalidate old requests. Accepting names both objects and the team-wide consequences in the shared confirmation dialog. Rejecting keeps the objects separate. Both actions refresh the list, retain the filter and announce success through MessagePlugin. Viewers see source evidence and status; contributor and administrator mutations remain subject to server authorization.

Evidence cards and the source snapshot drawer display complete or partial extraction with processed and total segment counts. The same model-call table shows stage, tier and count in evidence and processing details. Existing native date fields remain platform owned as documented above. No new date picker, scrollbar owner or runtime token system is introduced.

## P4 source verification

Duplicate notices use the current intake response to distinguish near duplicates. Historical repost records do not determine that message. Viewing the existing message uses the current knowledge base's document-opening event or the existing knowledge_id route parameter. A manual editor closes only after that explicit action. Source lists show publisher, original URL, publication time or a missing marker, and the matching method. Opening another message clears the old source list and ignores responses for the previous selection.

Message details fill the viewport at widths of 720px or less. The desktop drawer width remains the user's preferred width, including after a narrow viewport is restored to desktop. Narrow details hide the desktop resize handle. At every width, padded detail content stays within the drawer and long source URLs wrap without horizontal drawer scrolling. Original source links, reposts, evidence, report citations and anomaly links retain their full address and share the same wrapping behavior on desktop and narrow screens. Code and tables may keep their own bounded scrolling regions. The anomaly drawer also stays within the viewport while retaining its desktop width.

Adapter failures display a localized type with site, source URL, adapter and reason. All source URLs pass through the shared absolute HTTP(S) URL helper; other values remain plain escaped text. Report bodies use the existing Markdown rendering and sanitization path with Wiki conversion disabled. Valid report citation buttons open the existing evidence snapshot and preserve its quote; code examples and link labels keep their literal citation text, including nested labels such as [[C1]](URL). Wiki pages retain their existing Wiki link conversion. Download actions disable concurrent downloads and release temporary links and object URLs after completion or failure. Browser checks for this fix are deferred to the separately requested review.
