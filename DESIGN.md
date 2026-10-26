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

Entity details retain the shared metadata controls and source-material list. EntityActivity derives a one-hop directed graph from visible facts, with native node and edge buttons and a fact sidebar. It reuses semantic borders and the green center accent, and uses the existing TDesign date range, selection, loading and pagination controls. Wide detail panels place graph and timeline beside each other; narrow panels stack them. Graph navigation adds no dependency, decorative motion or theme tokens.

Material lists and search retain the existing library scope, publication date and star controls. Classification tags, topic controls, similar-report disclosures and the self-check pages are removed. Shared TDesign controls, spacing and feedback retain their established owners.

Entity candidates extend the entity workbench as a compact occurrence-ranked list and a bordered source review area. Material sources show their title link and literal quotation; fact sources retain their event link and endpoint frame. The existing name, action and source-link hierarchy, TDesign selects and buttons, locale bundles and semantic theme tokens remain the visual owners. Pending and ignored states share the same layout; review errors remain next to their action without replacing the source evidence.
