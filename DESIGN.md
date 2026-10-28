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
