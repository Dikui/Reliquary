# Reliquary UI behavior

## Product context

Reliquary serves a small team that collects source material and checks entities, facts, events and evidence. The supported locales are zh-CN and en-US; existing locale files own product copy. Light and dark themes retain the same hierarchy. The accessibility target for new controls is WCAG 2.2 AA.

## Business-context sources

| Scope | Source |
| --- | --- |
| Team visibility and roles | CONTEXT.md and docs/adr/0003-single-team-space.md |
| Global entity identity and source attribution | docs/adr/0002-global-entity-registry.md |
| Domain templates and field consolidation | docs/adr/0004-domain-templates-open-fields.md |
| Built in domains and deletion restrictions | .scratch/architecture-reshape/spec.md, phase P2 |

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Select/Listbox | TDesign Select and TagInput | Existing TDesign installation and locale provider | Filtered, multiple and tag input | Frontend type check; browser verification required when available |
| Date | Existing typed storage with native date input | KnowledgeWorkbench.vue | Existing native date picker accepts platform locale and geometry | Frontend type check |
| Form | TDesign inputs and application validation | DomainFieldEditor.vue and domainTemplate.ts | Domain creation, template editing, field consolidation | domainTemplate.test.ts |
| Scrollbar | Global application theme stylesheet | DESIGN.md and frontend/src/assets/theme/theme.css | Stable gutter for bounded regions | Premium static audit; browser verification required when available |
| Toast | TDesign MessagePlugin | Existing settings workflows | Success, warning, error and information | Frontend type check |
| CRUD | Knowledge domain API and settings view | DomainSettings.vue | Return to list on domain save; stay on consolidation after mapping | Template validation tests and backend domain tests |

## Flow ledger

| Operation | Pending | Success | Failure recovery |
| --- | --- | --- | --- |
| Create or edit domain | Form controls and save action disabled | Return to domain list with success toast | Keep the draft and show inline error |
| Delete domain | Shared useConfirmDelete dialog and busy action | Refresh and clamp the current list page | Keep list visible and show error; server prevents referenced domain deletion |
| Map or create a template field | Dialog controls disabled | Refresh selected domain and open-field statistics, remain in consolidation | Keep dialog values and show inline error |
| Query context | Existing loading controls | Keep query and show source-labelled results | Preserve query, show error and allow retry |
| View entity | Clear previous detail and show loading indicator | Show facts and events from all team knowledge bases | Preserve selection and show error |

Built in domain deletion stays disabled with an explanation. Viewers can browse domains; only owner and admin roles see mutations. The server remains authoritative for permissions.

## Dataset and draft state

The settings dialog uses transient local search, tab and pagination state. Domain and open-field tables show ten rows per page. Context lists use explicit load more. Scope changes reset scoped results and invalidate requests; an old response cannot replace results from a later scope. Domain template editing uses an independent copy of saved data. Leaving a dirty template requires the existing application confirmation dialog. Field consolidation is available after template changes are saved or cancelled.

## Layout and interaction

The settings content owns vertical scrolling. Long template forms keep their natural height. New controls use TDesign keyboard behavior, visible focus, disabled and busy states, and application-owned dialogs. New icon-only actions have localized labels. Chinese input composition does not submit context queries. Textareas resize through their existing autosize owner and cannot be manually resized.

Browser checks cover the changed flows, both locales and themes, keyboard input, narrow viewports, open select popups and failure recovery when browser tooling is available. Static checks and unit tests alone do not prove these runtime properties.
