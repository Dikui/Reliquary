---
version: alpha
name: Reliquary
description: Personal material library using Swiss information design.
---

# Reliquary design context

Reliquary is a reading and maintenance workspace for a single user. The approved
visual identity uses a strict grid, strong Helvetica headings, square controls,
neutral surfaces and library color marks. It supports zh-CN and en-US, light and
dark themes. CONTEXT.md and UX-CONTRACT.md own business and interaction rules.

`frontend/src/styles/tokens.css` is the canonical runtime token source.
`frontend/src/styles/global.css` supplies the document baseline, native control
inheritance and scrollbars. `useTheme` sets `theme-mode`; `useFont` preserves the
user's explicit font and scale preferences. The default font resolves to the
shared tokens. The root size is 14px; layout dimensions use pixel tokens so the
approved 16px design scale remains consistent.

## Swiss UI foundation

All application pages use the approved Swiss redesign. Shared primitives in
`frontend/src/components/ui` use the foundation below. The development preview
is `/platform/dev/ui`; it is excluded from production. Local mockups in
`design/` supply the visual reference. This foundation performs no business data migration.

### Colors

[tokens.css](frontend/src/styles/tokens.css) is the runtime source of truth.
It preserves the local mockup palette. This document describes the mapping and
does not generate another copy of the tokens. `useTheme` resolves light, dark
and system preferences into the root `theme-mode` attribute.

`--bg`, `--surface`, `--surface-soft`, `--text`, `--muted` and `--border` describe
surfaces and content. `--accent` is emphasis; `--danger`, `--success` and
`--warning` describe status. `--lib-1` through `--lib-8` identify libraries,
without semantic status. `--select` marks one current focus; checked rows use
`--surface-soft`. Status controls always have a text label.

### Typography

`--font` is the Helvetica and CJK fallback stack. `--mono` is for identifiers
and numbers. Components use the token type scale and line height. Explicit page font preferences remain available in general settings.

### Layout

The spacing scale runs from 4px to 48px. Controls have a 40px minimum height. Dialogs fit the viewport; content scrolls while actions remain reachable.
Tables own horizontal overflow without imposing a page height. Table callers
own paging and supply the current rows; the foundation does not fetch data.

The platform shell now uses the Swiss sidebar at the 224px `--sidebar` width.
Its brand, command entry, main routes, library list and diagnostics share the
mockup rhythm. Active navigation uses a `--text` background with a `--bg`
foreground in both themes. Library marks use the persisted color through
`libraryColorVar`. The optional collapsed
rail is 72px wide. At 640px and below, a top bar opens the shared left drawer.
The route outlet retains its flex width, height chain and page-owned scrolling.

### Elevation & Depth

Static surfaces have no shadow. Dialogs, drawers, menus, tooltips and notifications
use `--shadow-float`. Portals attach overlays to the body. Layer order is overlay
200, dialog 201, menu 210, tooltip 220 and toast 230.

### Shapes

Corners are square. Rules and borders provide hierarchy. Native select, checkbox
and radio interaction remains owned by the operating system; CSS supplies color
and sizing. Native textarea resizing remains vertical, as in the approved mockup.

### Components

Import reusable components from `frontend/src/components/ui`. Its index includes
the shared stylesheet. Each component accepts standard HTML attributes where
applicable. New styles are scoped by `rq-` classes; the global stylesheet owns document defaults.

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Table Selection | DataTable.vue | controlled selected and focused keys | current page rows | DataTable.test.ts |
| Select/Listbox | Select.vue, Combobox.vue | native select or Reka combobox | native, filtered | controls.test.ts, overlays.test.ts |
| Form | Field.vue and native controls | Field context associates label, hint and error | text, textarea, select, checkbox, switch, radio | controls.test.ts |
| Toast | feedback.ts and FeedbackHost.vue | one app host | success, info, warning, error | feedback.test.ts |

Buttons expose primary, default, ghost and danger variants. Busy buttons preserve
their label geometry and block repeated actions. Field wraps one control; an
explicit control ID must also be supplied to Field. Inputs preserve IME composition.
Tabs, dropdowns, tooltips, dialogs and comboboxes delegate keyboard and focus
behavior to Reka UI 2.11.0. Dialogs and drawers restore focus on close.
Drawers support left or right placement and a content class for the navigation
layout. Expanded navigation omits tooltips; the collapsed rail exposes the same
link names and count labels through keyboard-accessible tooltips and aria labels.

`toast.success/info/warning/error(content, durationMs?)` returns `{ close }`.
Duration zero persists until closed. Repeated identical active messages deduplicate.
`confirm({ header, body, confirmBtn, cancelBtn, onConfirm, onCancel, onClose })`
returns `{ destroy, update, result }`. `result` resolves to a boolean. Async
confirmation blocks duplicate actions, keeps a failed dialog open for retry,
and initially focuses Cancel. `destroy()` is idempotent. All application confirmations and toasts use this service.

Skeleton pulse and busy rotation respect reduced motion. All new visible text
and accessible action labels come from the application locale or caller props.
Use slots for page-specific table cells and recovery actions.

### Do's and Don'ts

- Use one yellow current item and independent gray checked rows.
- Use library colors only to distinguish libraries, with names alongside them.
- Keep links underlined and dark; use red on hover.
- Do not use mockup sample numbers as application data.
- Keep document defaults in the global stylesheet and control styling in shared primitives.
- Verify the development-only `/platform/dev/ui` route manually before a visual release.


### Swiss materials page

The materials route uses a two-column list and preview at widths above 900px.
The compact list uses one yellow focus row; the table uses DataTable's independent
focus and checked state. Narrow layouts retain an explicit Open button on every
list row. Full details open the independent material reading route. The page and
batch dialog use the shared Swiss controls and feedback service. Source labels and trust markers remain visible.

The header identifies the selected library with its persisted LibMark and its
catalogue position. Counts come from the current server result. Missing per-entry
entity/relationship counts remain unknown, without substituting fixture numbers.
The API currently supplies newest-collected ordering only. Native select and date
controls retain operating-system popup behavior; the page adds no custom calendar.


Focused rows share the `rq-focus-row` palette with DataTable: all descendant text
uses `--on-select`, including status labels, muted text and tags. Focused tags
have a transparent background; status squares retain their semantic colors.
The materials page can scroll when its expanded filters exceed the viewport,
while its material scroller retains at least 240px of height. Table title content
keeps a 266px minimum width and the shared table container owns horizontal overflow.

### Swiss material details

The detail route `/platform/entries/:id` has a wrapping action bar and a reading
column beside a 352px information rail. Below 960px the rail follows the body.
The body uses a 17px sans face with 1.85 line height and a 36em measure. Real
saved-body paragraph numbers occupy the margin and the referenced paragraph
alone uses `--select` and `--on-select`. The existing DocContent renderer owns
Markdown, formulas, diagrams, highlighting and protected attachment access.
The rail uses shared Tabs for summary, entities, relations and processing.
Native content-type selection, shared confirmation/toast services, and the
shared Dialog for body-image previews keep interaction ownership consistent.
Hidden Tabs panels never participate in layout, even when a page defines flex
layout for its active panel.

### Swiss search

The search measure follows the local mockup at 1024px. Library color buttons and
quick filters share one wrapping row. Advanced entity and topic filters use
compact native selects with a placeholder and removable selected values; empty
option lists use a short explanation instead of an empty list box. Reset appears
only when the non-library filters differ from their defaults.

Search snippet matches are an approved exception to the single yellow focus
rule: each matching `mark` uses `--select` with `--on-select` text, as in the
search mockup. Focused results continue to use the same accessible foreground.
The runtime source remains `frontend/src/styles/tokens.css`.

### Swiss entities

`/platform/entities` uses a 304px entity list beside the detail area. The current
entity uses the text/background inversion; pointer or keyboard focus on another
row uses the selection foreground and background. Below 900px the list precedes
the details and remains scrollable. Empty lists do not reserve a blank detail pane.

Cytoscape owns graph layout, zoom and pan. Rectangular nodes use neutral library
independent colors: the center inverts text/background, direct neighbors use the
page background with a text border, and more distant nodes use muted text. Only
the selected edge uses yellow, including its label background and `--on-select`
foreground. The same semantic tokens resolve both themes. Source evidence follows
the graph; keyboard users can select nodes and edges through a semantic button list.

The related materials table keeps a 680px minimum width inside its own horizontal
scroller. It shows eight ID characters with the complete ID in the title; ID,
type and collected-date cells do not wrap into character columns. Graph relation
type filters and the 20/50/100 limit share the wrapping toolbar with hop controls.


### Swiss topics

The topics route uses the mockup's 304px list beside a detail column capped at
1024px. Below 900px the list precedes the details. Empty lists occupy one column
without allocating an unused detail panel. Selected topics invert text and
background; pointer and keyboard focus use the shared selection foreground.
The historical assignment bar splits successful and failed work into neutral
and danger segments, with real processed/total counts and readable batch errors.

Topic forms, follow controls, confirmations and notifications reuse the Swiss
owners. Materials use a semantic table with a 760px minimum width inside its
own horizontal scroller. IDs show eight characters with the full ID in the title;
links retain the full ID. The title column is at least 220px and dates do not wrap.
Library colors come from the catalog, alongside the library's actual name.

Topic layout sizes use pixels or the shared pixel-based tokens, independent of
the legacy 14px root font. The progress bar uses the 12px spacing token. Material
titles have 700 weight and content types use the shared Tag with localized
built-in names; custom library type names are preserved.

### Swiss diagnostics

Ingest failures use a 1280px reading area, a wrapping filter form and a table
with a 1180px minimum width in its own horizontal scroller. Failure details use
the shared right drawer at 544px. Material tabs only appear for captured data;
raw text and images keep their existing authenticated read and release behavior.

Check records use a 272px batch list beside grouped, numbered problems and gray
source quotations. The current quotation uses the shared focus colors. Below
900px the batch list precedes the findings. Both diagnostic pages keep 24px of
top padding below the mobile navigation. Native date and time controls retain
platform-owned pickers, while the shared dialog owns weekly schedule editing.

### Swiss evaluation and model calls

Evaluation and model calls share a 1280px content measure and 24px mobile top
clearance. Evaluation has three tabs, with question and candidate editing in
shared dialogs. Five metrics use large monospaced values; comparison deltas use
success and danger tokens with explicit signs. Reference links use the material
detail route, retaining full IDs in titles when labels are shortened.

Model calls show actual aggregate values above daily column charts and grouped
and detailed tables. Each currency has a separate plot with a common scale;
calls stack successful and failed requests in text and danger colors. Token
and cost plots show the available totals without estimating a failure split.
Tables scroll within their own containers. The call drawer overrides the shared
content viewer's legacy styles with square neutral surfaces in both themes.

## Swiss command palette

The global palette uses the shared Dialog at layer 2400, its retrieval drawer at
2450, and its image preview at 2500. The panel is 640px wide, begins at 12vh, and
fits narrow screens with 16px margins. A two-pixel rule, square corners and the
floating shadow match the local design reference. Results scroll within the
panel; the scope row and keyboard footer remain outside that scroll area.
Only the current result has a yellow background, with every descendant using
`--on-select`. Library marks and content-type Tags reuse the shared primitives.
Retrieval sliders flex into the available width while their numeric outputs
reserve 56px without wrapping, including in the narrow drawer layout.

## Swiss settings groups

Settings share `frontend/src/styles/settings-form.css`: a 224px description
column, a flexible control column, two-pixel section rules, and one column
below a 720px content width. Each group inherits the page heading. Model and
API editors use the shared Dialog or Drawer; pending confirmations close when
their owning view is disposed. Tables scroll within the shared DataTable.
Native select and local date/time inputs keep platform popup behavior. API key
expiry preserves the existing local-time conversion. Textareas retain the
approved vertical resize behavior. Shared tokens remain the color and type
source of truth; these groups no longer use TDesign theme adapters.

## Completed cleanup

No component-library theme adapter or icon library remains. Collapsed navigation
uses short text marks; mobile navigation uses small inline SVGs. Only the reading
presentation remains in DocContent. Calendar dates preserve their calendar day;
timestamps retain timezone conversion through `utils/dateTime.ts`.

### Managed entity controls

Managed-library settings, entity editors, association controls and history reuse
the existing Swiss controls, spacing and tokens. They introduce no new visual
system. Entity and relation forms use shared Dialog, native select/date controls,
and the shared feedback service. Wide schema rows wrap on narrow screens;
entity details retain the existing workbench scroll ownership.

### Editable product shelf

Managed libraries enable a shelf through the existing settings draft and Save action.
The server installs the eight fixed categories and relationship rules. Module types,
regions and ordered stages use the same labelled inputs and shared buttons as other
settings; their names are user data. Stable keys survive renaming and reordering.

The shelf uses the Swiss tokens, a four-column hierarchy list and shared editor
dialogs. It no longer has a relation graph or candidate-parent vocabulary. The
existing detail tabs display parts, certifications and document records while the
later status-table workflow is implemented. Below 1100px details follow the list.
These views contain no bundled business data.

Shelf model details use the full reading area with hierarchy breadcrumbs, stage
and checklist totals, native radio groups for optional modules, and stacked
composition, sale, certification, checklist, parameter, file and history sections.
Semantic tables own their bounded overflow. Component and ancestor certifications
have separate row groups. The page uses the existing tokens, shared feedback
states and buttons; it adds no palette or interaction primitive.

Shelf upper-node and part details reuse the model page's Swiss grid, typography,
semantic tables and local horizontal scrolling. Upper nodes show descendant
models, a requirement matrix, files owned at this node and certification origins.
Parts show configured attributes, reverse model usage, module requirements and
owned certifications. Product stage selection uses a native labelled control.
No new palette, font, overlay or responsive layout is introduced.

Shelf review uses a 304px changeset list beside nine collapsible conflict groups.
The current changeset uses the shared selection colors. Decisions use compact
square buttons, payload fields use the shared native controls and Combobox, and
the sticky footer keeps progress, blocking omissions and confirmation visible.
It retains desktop layout and the existing token, feedback and scrolling owners.
