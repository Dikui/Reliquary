---
version: alpha
name: Reliquary
description: Personal source-material library with compact TDesign workflows and a restrained green navigation accent.
colors:
  primary: "#07c05f"
  surface: "#ffffff"
  page: "#eeeeee"
  danger: "#e34d59"
  warning: "#ed7b2f"
typography:
  sans:
    fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif'
  mono:
    fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace'
rounded:
  sm: "2px"
  md: "6px"
  lg: "9px"
omitted:
  - section: spacing
    reason: Existing component and page spacing remains canonical; this feature does not introduce a global spacing scale.
components:
  button: {}
  input: {}
  date: {}
  pagination: {}
---

# Reliquary design context

## Overview

This is an existing product interface for a single user collecting, reading and maintaining source material. The reference is its current library, operation-log and settings screens: a reading workspace with compact controls, bordered records and visible technical identifiers. It is not a marketing surface or a new visual identity.

Supported locales are zh-CN and en-US. No Japan-market requirement is established. All product and accessibility copy belongs in the locale bundles. See CONTEXT.md and UX-CONTRACT.md for business and behavior rules.

The signature is the existing green selection and action accent. Routine diagnostics stay restrained so source content, errors and identifiers remain easy to scan. Avoid decorative metrics, new palettes and display fonts.

Runtime tokens remain canonical (mapping model B): frontend/src/assets/theme/theme.css → frontend/src/main.ts → TDesign and var(--td-*) consumers. This file mirrors those values; it does not generate a competing token system. User font preferences override defaults through frontend/src/composables/useFont.ts.

## Colors

| Document token | Runtime token | Consumers |
| --- | --- | --- |
| primary | --td-brand-color / light --td-brand-color-4 | Active navigation and focus |
| surface | --td-bg-color-container | Cards and controls |
| page | --td-bg-color-page | Page background |
| danger | --td-error-color | Error messages |
| warning | --td-warning-color | Incomplete or unknown results |

Use semantic tokens in components. Dark theme remaps those same roles in theme.css; it does not change hierarchy. Body and secondary text use --td-text-color-primary/secondary. Borders use --td-component-stroke. The shared theme owns scrollbar colors, geometry and forced-colors behavior.

## Typography

The default sans and mono stacks above mirror --app-font-family and --app-font-family-mono. Keep Chinese-capable fallbacks and user-selected font preferences. Existing body/control copy is 14px, supporting text 13px and page headings 24px. IDs and raw materials use the mono role, wrap when possible and retain bounded local scrolling.

## Layout

Preserve the platform sidebar and existing page-owned scrolling. Diagnostic list/detail content uses the operation-log rhythm: 20px record padding, 16px gaps and responsive wrapping controls. At 640px and below, padding reduces and label/value pairs stack. Do not impose table heights on shared layout or settings forms.

## Elevation & Depth

Use borders and tonal surfaces for ordinary records and raw materials. TDesign owns overlay elevation. New diagnostics add no decorative shadows or motion.

## Shapes

Use runtime radius tokens: sm → --td-radius-small, md → --td-radius-medium, lg → --td-radius-large. Do not add a separate radius system.

## Components

TDesign owns buttons, inputs, date pickers, loading, empty states and pagination. SideMenu.vue owns grouped navigation, and the existing theme owns focus and scrollbars. Native links retain visible keyboard focus. Loading actions keep their label and geometry. Inline errors retain readable results and provide retry. Material text is escaped, and authenticated image responses become short-lived Blob URLs that are released on replacement or unmount.

## Do's and Don'ts

- Reuse existing semantic tokens and TDesign behavior across both locales and themes.
- Keep unknown, partial and expired states explicit; link to existing diagnostic owners.
- Do not add replay or destructive actions to the read-only failure viewer.
- Do not infer a resolved failure from an accepted recovery submission.


EntryProcessingStatus uses the existing success outline tag for confirmed storage and a separate neutral, active or error tag for background work. Cards, rows and details share these labels and the TDesign retry button. Labels wrap in narrow containers; the saved state does not change to an error color when enrichment fails. No new theme tokens are introduced.

## Swiss UI foundation

The approved Swiss redesign is introduced in stages. Existing TDesign pages keep
the preceding contract until their own migration. New shared primitives in
`frontend/src/components/ui` use the foundation below. The development preview
is `/platform/dev/ui`; it is excluded from production. Local mockups in
`design/` supply the visual reference. This foundation performs no business
data migration and does not migrate existing TDesign call sites.

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
and numbers. Components use the token type scale and line height. Existing page
font preferences and TDesign variables are independent during the migration.

### Layout

The spacing scale runs from 0.25rem to 3rem. Controls have a 2.5rem minimum
height. Dialogs fit the viewport; content scrolls while actions remain reachable.
Tables own horizontal overflow without imposing a page height. Table callers
own paging and supply the current rows; the foundation does not fetch data.

The platform shell now uses the Swiss sidebar at the 224px `--sidebar` width.
Its brand, command entry, main routes, library list and diagnostics share the
mockup rhythm. Active navigation uses a `--text` background with a `--bg`
foreground in both themes. Library marks use the persisted color through
`libraryColorVar`. The optional collapsed
rail is 72px wide. At 640px and below, a top bar opens the shared left drawer.
Only the shell has migrated: the existing route outlet retains its flex width,
height chain and page-owned scrolling, and its content keeps the TDesign theme.

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
applicable. New styles are scoped by `rq-` classes; tokens add no element resets.

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
and initially focuses Cancel. `destroy()` is idempotent. Existing TDesign call
sites are not migrated by this foundation.

Skeleton pulse and busy rotation respect reduced motion. All new visible text
and accessible action labels come from the application locale or caller props.
Use slots for page-specific table cells and recovery actions.

### Do's and Don'ts

- Use one yellow current item and independent gray checked rows.
- Use library colors only to distinguish libraries, with names alongside them.
- Keep links underlined and dark; use red on hover.
- Do not use mockup sample numbers as application data.
- Do not introduce element-wide resets while old pages remain in use.
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

The search measure follows the local mockup at 64rem. Library color buttons and
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
