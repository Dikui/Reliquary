<template>
  <t-tooltip :content="t('knowledgeBase.infoCard.tooltip')" placement="top">
    <t-popup
      v-if="kbInfo"
      trigger="click"
      placement="bottom-right"
      :overlay-style="{ padding: 0 }"
      :overlay-inner-style="{ padding: 0 }"
    >
      <template #content>
      <div class="kb-info-card" :lang="locale">
        <div class="kb-info-card-header">{{ t('knowledgeBase.infoCard.title') }}</div>
        <div class="kb-info-card-body">
          <section class="setting-drawer__section">
            <h4 class="setting-drawer__section-title">
              {{ t('knowledgeBase.infoCard.basic') }}
            </h4>

            <div v-if="kbInfo.description" class="kb-info-card-row">
              <span class="kb-info-card-label">{{ t('knowledgeBase.description') }}</span>
              <span class="kb-info-card-value kb-info-card-value-block">{{ kbInfo.description }}</span>
            </div>
            <div v-if="kbInfo.created_at" class="kb-info-card-row">
              <span class="kb-info-card-label">{{ t('knowledgeBase.infoCard.createdAt') }}</span>
              <span class="kb-info-card-value">{{ formatStringDate(new Date(kbInfo.created_at)) }}</span>
            </div>
            <div v-if="hasDistinctUpdate" class="kb-info-card-row">
              <span class="kb-info-card-label">{{ t('knowledgeBase.accessInfo.lastUpdated') }}</span>
              <span class="kb-info-card-value">{{ lastUpdatedLabel }}</span>
            </div>
            <div v-if="supportedFileTypesSorted.length" class="kb-info-card-row">
              <span class="kb-info-card-label">{{ t('knowledgeBase.infoCard.supportedFileTypes') }}</span>
              <span class="kb-info-card-value">
                <span
                  v-for="ft in supportedFileTypesSorted"
                  :key="ft"
                  class="kb-info-card-ext"
                >.{{ ft }}</span>
              </span>
            </div>
          </section>

          <section v-if="capabilities.length" class="setting-drawer__section">
            <h4 class="setting-drawer__section-title">
              {{ t('knowledgeBase.infoCard.capabilities') }}
            </h4>
            <div class="kb-info-card-row">
              <span class="kb-info-card-label">{{ t('knowledgeBase.infoCard.enabled') }}</span>
              <span class="kb-info-card-value">
                <t-tag
                  v-for="cap in capabilities"
                  :key="cap.key"
                  size="small"
                  variant="light"
                  :theme="cap.theme"
                >
                  {{ cap.label }}
                </t-tag>
              </span>
            </div>
          </section>
          <section v-if="chunkingRows.length" class="setting-drawer__section">
            <h4 class="setting-drawer__section-title">
              {{ t('knowledgeBase.infoCard.chunking') }}
            </h4>
            <div
              v-for="row in chunkingRows"
              :key="row.key"
              class="kb-info-card-row"
            >
              <span class="kb-info-card-label">{{ row.label }}</span>
              <span class="kb-info-card-value">{{ row.value }}</span>
            </div>
          </section>
          <section v-if="statRows.length" class="setting-drawer__section">
            <h4 class="setting-drawer__section-title">
              {{ t('knowledgeBase.infoCard.stats') }}
            </h4>
            <div
              v-for="stat in statRows"
              :key="stat.key"
              class="kb-info-card-row"
            >
              <span class="kb-info-card-label">{{ stat.label }}</span>
              <span class="kb-info-card-value kb-info-card-value-number">{{ stat.value }}</span>
            </div>
          </section>

        </div>
      </div>
    </template>
    <button
      type="button"
      class="kb-info-button"
    >
      <t-icon name="info-circle" size="16px" />
    </button>
  </t-popup>
  </t-tooltip>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatStringDate } from '@/utils'

const props = defineProps<{
  // The KB detail / list object. Typed as any to avoid coupling to the
  // backend's exhaustive shape; the popover reads optional fields and
  // falls back gracefully when they are missing.
  kbInfo: any
  // Optional pre-computed list of supported file extensions (e.g.
  // ["pdf", "docx", …]). The popover does not fetch parser engines on
  // its own — call sites that already know which formats are reachable
  supportedFileTypes?: string[]
}>()

const { t, locale } = useI18n()
// KB.UpdatedAt is auto-bumped by GORM only when the KB row itself is
// touched (rename, config edit, …). For a freshly created KB whose
// only mutations were document uploads, updated_at == created_at and
// surfacing "last updated" alongside "created at" reads as
// duplicated noise. Hide the row in that case and let it reappear
// once the KB metadata is actually edited.
const hasDistinctUpdate = computed<boolean>(() => {
  const created = props.kbInfo?.created_at
  const updated = props.kbInfo?.updated_at
  if (!updated) return false
  if (!created) return true
  return new Date(updated).getTime() !== new Date(created).getTime()
})

const lastUpdatedLabel = computed<string>(() => {
  const raw = props.kbInfo?.updated_at
  return raw ? formatStringDate(new Date(raw)) : ''
})

const supportedFileTypesSorted = computed<string[]>(() => {
  if (!props.supportedFileTypes?.length) return []
  return [...props.supportedFileTypes].sort()
})

type CapabilityTheme = 'primary' | 'success' | 'warning' | 'default'
const capabilities = computed<Array<{ key: string; label: string; theme: CapabilityTheme }>>(() => {
  const kb: any = props.kbInfo
  if (!kb) return []
  const items: Array<{ key: string; label: string; theme: CapabilityTheme }> = []
  {}
  return items
})

const chunkingStrategyLabel = computed<string>(() => {
  const raw: string = (props.kbInfo?.chunking_config?.strategy || '').toLowerCase()
  const key = (raw === '' || raw === 'recursive') ? 'legacy' : raw
  const path = `knowledgeEditor.chunking.strategies.${key}.label`
  const translated = t(path)
  return translated === path ? raw : translated
})

const chunkingRows = computed<Array<{ key: string; label: string; value: string }>>(() => {
  const kb: any = props.kbInfo
  if (!kb) return []
  const cfg = kb.chunking_config
  if (!cfg) return []
  const rows: Array<{ key: string; label: string; value: string }> = []
  if (chunkingStrategyLabel.value) {
    rows.push({
      key: 'strategy',
      label: t('knowledgeEditor.chunking.strategyLabel'),
      value: chunkingStrategyLabel.value,
    })
  }
  if (typeof cfg.chunk_size === 'number' && cfg.chunk_size > 0) {
    rows.push({
      key: 'size',
      label: t('knowledgeEditor.chunking.sizeLabel'),
      value: `${cfg.chunk_size} ${t('knowledgeEditor.chunking.characters', cfg.chunk_size)}`,
    })
  }
  if (typeof cfg.chunk_overlap === 'number') {
    rows.push({
      key: 'overlap',
      label: t('knowledgeEditor.chunking.overlapLabel'),
      value: `${cfg.chunk_overlap} ${t('knowledgeEditor.chunking.characters', cfg.chunk_overlap)}`,
    })
  }
  if (cfg.enable_parent_child) {
    const parent = cfg.parent_chunk_size || 4096
    const child = cfg.child_chunk_size || 384
    rows.push({
      key: 'parent-child',
      label: t('knowledgeEditor.chunking.parentChildLabel'),
      value: `${t('knowledgeBase.infoCard.parentShort')} ${parent} / ${t('knowledgeBase.infoCard.childShort')} ${child}`,
    })
  }
  if (typeof cfg.token_limit === 'number' && cfg.token_limit > 0) {
    rows.push({
      key: 'token-limit',
      label: t('knowledgeEditor.chunking.tokenLimitLabel'),
      value: String(cfg.token_limit),
    })
  }
  return rows
})

const statRows = computed<Array<{ key: string; label: string; value: number | string }>>(() => {
  const kb: any = props.kbInfo
  if (!kb) return []
  const items: Array<{ key: string; label: string; value: number | string }> = []
  // user-facing entry total. Document KBs use entry_count for the
  // file-level total (chunk_count there counts internal splits and is
  // not meaningful to surface here). Mirrors the same branching used
  // by the list card.
  if (typeof kb.entry_count === 'number') {
    items.push({
      key: 'entry',
      label: t('knowledgeBase.infoCard.documentCount'),
      value: kb.entry_count,
    })
  }
  return items
})
</script>

<style scoped lang="less">
/* Info button is intentionally lighter than the sibling settings
   button: text-only by default, with a soft circular hover state.
   Two identical filled circles next to each other read as a stamp,
   so the info trigger sits closer to a typical "auxiliary glyph
   next to a title" and yields visual hierarchy to the settings
   gear (the primary action). */
.kb-info-button {
  position: relative;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 50%;
  background: transparent;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
  transition: background 0.2s ease, color 0.2s ease;
  padding: 0;

  &:hover:not(:disabled) {
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-primary);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.4;
  }

  &.has-warning {
    color: var(--td-error-color);
  }

  &.has-warning::after {
    content: '';
    position: absolute;
    top: 2px;
    right: 2px;
    width: 7px;
    height: 7px;
    background: var(--td-error-color);
    border-radius: 50%;
    border: 1.5px solid var(--td-bg-color-container, #fff);
  }

  :deep(.t-icon) {
    font-size: 16px;
  }
}

.kb-info-card {
  min-width: 280px;
  max-width: 360px;
  /* Cap to ~70% of the viewport so the popup never overflows the screen
     on shorter laptops. Internal scroll keeps the header pinned via the
     sticky rule below. */
  max-height: min(70vh, 560px);
  display: flex;
  flex-direction: column;
  padding: 12px 16px;
  font-size: 12px;
  color: var(--td-text-color-primary);
  overflow: hidden;
}

.kb-info-card-header {
  flex: 0 0 auto;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--td-component-stroke);
}

/* Scrolling body so the popover never grows past max-height. Negative
   side margins line the scrollbar up with the card edge while keeping
   the row labels aligned with the header. */
.kb-info-card-body {
  flex: 1 1 auto;
  overflow-y: auto;
  margin: 0 -16px;
  padding: 0 16px;
  display: flex;
  flex-direction: column;
}

.kb-info-card-body .setting-drawer__section {
  padding: 12px 0 16px;
  border-bottom: 1px solid var(--td-component-stroke);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.kb-info-card-body .setting-drawer__section:first-child {
  padding-top: 0;
}

.kb-info-card-body .setting-drawer__section:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.kb-info-card-body .setting-drawer__section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--td-text-color-primary);
  margin: 0 0 4px;
  user-select: none;
  display: flex;
  align-items: center;
  gap: 8px;
}

.kb-info-card-body .setting-drawer__section-title::before {
  content: '';
  width: 3px;
  height: 14px;
  background: var(--td-brand-color);
  border-radius: 2px;
  flex-shrink: 0;
}

.kb-info-card-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 0;
  line-height: 1.6;
}

.kb-info-card-label {
  flex: 0 0 80px;
  color: var(--td-text-color-secondary);
}

.kb-info-card:lang(en) .kb-info-card-label {
  flex-basis: 112px;
  word-break: normal;
  overflow-wrap: break-word;
  white-space: normal;
  overflow: visible;
}

.kb-info-card:lang(en) .kb-info-card-value {
  min-width: 0;
}

.kb-info-card-value {
  flex: 1;
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  color: var(--td-text-color-primary);
  word-break: break-word;
}

.kb-info-card-value-block {
  display: block;
  color: var(--td-text-color-secondary);
  white-space: pre-wrap;
}

.kb-info-card-value-number {
  font-variant-numeric: tabular-nums;
  font-weight: 500;
}

.kb-info-card-value-mono {
  font-family: var(--td-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 11px;
  color: var(--td-text-color-secondary);
}

.kb-info-card-ext {
  display: inline-flex;
  align-items: center;
  padding: 1px 6px;
  border-radius: 3px;
  background: var(--td-bg-color-component, #f5f7fa);
  color: var(--td-text-color-secondary);
  font-family: var(--td-font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 11px;
  line-height: 1.4;
}

.kb-info-card-hint {
  color: var(--td-text-color-placeholder);
  font-size: 11px;
}
</style>
