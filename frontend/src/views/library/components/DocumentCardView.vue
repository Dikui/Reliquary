<script setup lang="ts">
import { ref, computed, nextTick, onBeforeUnmount, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { formatFileSize } from '@/utils/files';
import { isIngestAPIEntry } from '@/utils/entrySourceLabels';
import { markdownToPlainText } from '@/utils/markdownPlainText';
import DocumentActionMenu from './DocumentActionMenu.vue';
import DocumentMenuDelete from './DocumentMenuDelete.vue';
import EntryClassification from '@/components/EntryClassification.vue';
import EntryStarToggle from '@/components/EntryStarToggle.vue';
import SelectionCheckbox from '@/components/SelectionCheckbox.vue';
import type { ArticleJudgment } from '@/api/entry-judgments';
import type { ClassifiedEntry } from '@/types/entryClassification';
import EntryProcessingTimeline from '@/components/EntryProcessingTimeline.vue';

interface EntryCard extends ClassifiedEntry {
  id: string;
  library_id?: string;
  parse_status: string;
  description?: string;
  file_name?: string;
  original_file_name?: string;
  display_name?: string;
  title?: string;
  type?: string;
  updated_at?: string;
  file_type?: string;
  isMore?: boolean;
  metadata?: any;
  error_message?: string;
  source?: string;
  created_at?: string;
  file_size?: number | string;
  channel?: string;
}

const props = defineProps<{
  items: EntryCard[];
  selectedIds: Set<string>;
  batchMode: boolean;
  traceAvailableById: Record<string, boolean>;
  deleteEntry?: (item: EntryCard) => Promise<boolean>;
}>();

const emit = defineEmits<{
  (e: 'open', item: EntryCard): void;
  (e: 'open-entity', id: string): void;
  (e: 'judgment-updated', value: ArticleJudgment): void;
  (e: 'toggle-checkbox', id: string, checked: boolean, ctx?: { e?: Event }): void;
  (e: 'menu-visible-change', visible: boolean, item: EntryCard): void;
  (e: 'action', action: 'download' | 'view-trace' | 'reparse' | 'cancel-parse' | 'batch-manage' | 'delete', item: EntryCard): void;
  (e: 'deleted', id: string): void;
}>();

const { t } = useI18n();
const menuDelete = ref<InstanceType<typeof DocumentMenuDelete> | null>(null);

// --- Menu index tracking ---
const activeMenuIndex = ref(-1);
const openMenu = (index: number) => {
  activeMenuIndex.value = index;
};
const onMenuVisibleChange = (visible: boolean, item: EntryCard) => {
  if (!visible) {
    activeMenuIndex.value = -1;
  }
  emit('menu-visible-change', visible, item);
};

// --- Parse status helpers ---
const CANCELABLE_PARSE_STATUSES = new Set(['pending', 'processing', 'finalizing']);
const isParseInFlight = (status?: string): boolean =>
  CANCELABLE_PARSE_STATUSES.has(String(status ?? ''));

const isTraceMenuVisible = (item: EntryCard): boolean => {
  if (!item?.id) return false;
  if (isParseInFlight(item.parse_status)) return true;
  return props.traceAvailableById[item.id] === true;
};

const inFlightCardStatusText = (item: EntryCard): string => {
  if (item.parse_status === 'finalizing') {
    return t('library.statusFinalizing');
  }
  return t('library.parsingInProgress');
};

// --- Display helpers ---
const formatDocTime = (time?: string) => {
  if (!time) return '--';
  const d = new Date(time);
  if (Number.isNaN(d.getTime())) return '--';
  const yy = String(d.getFullYear()).slice(2);
  const MM = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${yy}-${MM}-${dd} ${hh}:${mm}`;
};

const getEntryType = (item: EntryCard) => {
  if (item.source_kind === 'mine' || item.source_kind === 'report') return t(`library.sourceKinds.${item.source_kind}`);
  if (item.channel === 'rss') return t('library.channelRSS');
  if (isIngestAPIEntry(item)) return t('library.typeIngestAPI');
  if (item.type === 'url') return t('library.typeURL') || 'URL';
  if (item.type === 'manual') return t('library.typeManual');
  if (item.file_type?.toLowerCase() === 'manual') return t('ikb18.markdown');
  if (item.file_type) return item.file_type.toUpperCase();
  return '--';
};

const channelLabelMap: Record<string, string> = {
  web: 'library.channelWeb',
  api: 'library.channelApi',
  browser_extension: 'library.channelBrowserExtension',
  rss: 'library.channelRSS',
};

const getChannelLabel = (channel: string) => {
  const key = channelLabelMap[channel];
  return key ? t(key) : t('library.channelUnknown');
};

// --- Card click handler ---
const onCardClick = (item: EntryCard) => {
  if (props.batchMode) {
    emit('toggle-checkbox', item.id, !props.selectedIds.has(item.id));
    return;
  }
  emit('open', item);
};

// --- Hover popover ---
const hoveredCardItem = ref<EntryCard | null>(null);
const cardPopoverPos = ref({ x: 0, y: 0 });
const CARD_POPOVER_OFFSET = 12;
const CARD_POPOVER_ESTIMATED_WIDTH = 360;
const CARD_POPOVER_ESTIMATED_HEIGHT = 300;
const cardHoverShowDelay = 300;
let cardHoverTimer: ReturnType<typeof setTimeout> | null = null;
let cardPopoverElement: HTMLElement | null = null;

const dismissCardPopover = () => {
  if (cardHoverTimer) {
    clearTimeout(cardHoverTimer);
    cardHoverTimer = null;
  }
  hoveredCardItem.value = null;
  cardPopoverElement = null;
};

const calculatePopoverPositionFromCard = (cardElement: HTMLElement): { x: number; y: number } => {
  const cardRect = cardElement.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;

  let popoverWidth = CARD_POPOVER_ESTIMATED_WIDTH;
  let popoverHeight = CARD_POPOVER_ESTIMATED_HEIGHT;

  if (cardPopoverElement) {
    const rect = cardPopoverElement.getBoundingClientRect();
    if (rect.width > 0) popoverWidth = rect.width;
    if (rect.height > 0) popoverHeight = rect.height;
  }

  let x = 0;
  let y = 0;

  // Strategy 1: right side
  const rightX = cardRect.right + CARD_POPOVER_OFFSET;
  if (rightX + popoverWidth <= viewportWidth - 10) {
    x = rightX;
    y = cardRect.top;
    if (y + popoverHeight > viewportHeight - 10) y = viewportHeight - popoverHeight - 10;
    y = Math.max(10, y);
    return { x, y };
  }

  // Strategy 2: left side
  const leftX = cardRect.left - popoverWidth - CARD_POPOVER_OFFSET;
  if (leftX >= 10) {
    x = leftX;
    y = cardRect.top;
    if (y + popoverHeight > viewportHeight - 10) y = viewportHeight - popoverHeight - 10;
    y = Math.max(10, y);
    return { x, y };
  }

  // Strategy 3: below
  const bottomY = cardRect.bottom + CARD_POPOVER_OFFSET;
  if (bottomY + popoverHeight <= viewportHeight - 10) {
    y = bottomY;
    x = cardRect.left;
    if (x + popoverWidth > viewportWidth - 10) x = viewportWidth - popoverWidth - 10;
    x = Math.max(10, x);
    return { x, y };
  }

  // Strategy 4: above
  const topY = cardRect.top - popoverHeight - CARD_POPOVER_OFFSET;
  y = Math.max(10, topY);
  x = cardRect.left;
  if (x + popoverWidth > viewportWidth - 10) x = viewportWidth - popoverWidth - 10;
  x = Math.max(10, x);
  return { x, y };
};

const onCardMouseEnter = (ev: MouseEvent, item: EntryCard) => {
  if (cardHoverTimer) {
    clearTimeout(cardHoverTimer);
    cardHoverTimer = null;
  }
  const cardElement = (ev.currentTarget as HTMLElement);
  cardHoverTimer = setTimeout(() => {
    cardHoverTimer = null;
    // Folder navigation can replace the card list before this delayed callback
    // runs. A detached card has a zero rect, which used to place the teleported
    // popover at the top-left corner of the viewport.
    if (!cardElement.isConnected || !props.items.some(candidate => candidate.id === item.id)) return;
    hoveredCardItem.value = item;
    const pos = calculatePopoverPositionFromCard(cardElement);
    cardPopoverPos.value = pos;
    nextTick(() => {
      if (!cardElement.isConnected || hoveredCardItem.value?.id !== item.id) return;
      cardPopoverElement = document.querySelector('.entry-card-hover-popover') as HTMLElement;
      if (cardPopoverElement) {
        const refinedPos = calculatePopoverPositionFromCard(cardElement);
        cardPopoverPos.value = refinedPos;
      }
    });
  }, cardHoverShowDelay);
};

const onCardMouseLeave = () => {
  dismissCardPopover();
};

// Refreshing the article collection may replace a card without necessarily
// dispatching mouseleave on a card that Vue removes.
watch(() => props.items, dismissCardPopover);
onBeforeUnmount(dismissCardPopover);

// --- Action handlers ---
const handleAction = (action: 'download' | 'view-trace' | 'reparse' | 'cancel-parse' | 'batch-manage' | 'delete', item: EntryCard) => {
  if (item.isMore !== undefined) item.isMore = false;
  activeMenuIndex.value = -1;
  emit('action', action, item);
};

const openDelete = async (item: EntryCard) => {
  item.isMore = false;
  activeMenuIndex.value = -1;
  dismissCardPopover();
  await nextTick();
  menuDelete.value?.open(item, props.deleteEntry ? () => props.deleteEntry!(item) : undefined);
};
</script>

<template>
  <div class="doc-card-view">
    <div class="doc-card-list doc-card-list-animated">

    <div
      class="entry-card"
      :class="{ 'is-selected': selectedIds.has(item.id), 'batch-mode': batchMode }"
      :data-select-id="item.id"
      v-for="(item, index) in items"
      :key="item.id"
      @click="onCardClick(item)"
      @mouseenter="onCardMouseEnter($event, item)"
      @mouseleave="onCardMouseLeave"
    >
      <div class="card-content">
        <div class="card-content-nav">
          <div v-if="batchMode" class="card-nav-check" @click.stop>
            <SelectionCheckbox
              class="card-select-checkbox"
              size="small"
              :checked="selectedIds.has(item.id)"
              :title="item.file_name"
              @change="(checked: boolean, ctx?: { e?: Event }) => emit('toggle-checkbox', item.id, checked, ctx)"
            />
          </div>
          <span class="card-content-title" :title="item.file_name">{{ item.file_name }}</span>
          <EntryStarToggle v-if="item.parse_status === 'completed'" :entry="item" @updated="emit('judgment-updated', $event)" />
          <t-popup

            v-model="item.isMore"
            overlayClassName="card-more"
            :on-visible-change="(v: boolean) => onMenuVisibleChange(v, item)"
            trigger="click"
            :destroy-on-close="false"
            placement="bottom-right"
          >
            <div
              variant="outline"
              class="more-wrap"
              @click.stop="openMenu(index)"
              :class="[activeMenuIndex === index ? 'active-more' : '']"
            >
              <img class="more-icon" src="@/assets/img/more-actions.png" alt="" />
            </div>
            <template #content>
              <!-- Normal menu -->
              <div  class="card-menu">
                <DocumentActionMenu
                  :item="item"
                  :trace-visible="isTraceMenuVisible(item)"
                  @view-trace="handleAction('view-trace', item)"
                  @reparse="handleAction('reparse', item)"
                  @cancel-parse="handleAction('cancel-parse', item)"
                  @batch-manage="handleAction('batch-manage', item)"
                  @delete="openDelete(item)"
                />
              </div>

            </template>
          </t-popup>
        </div>

        <div v-if="item.parse_status !== 'completed' && (item.acquisition === 'model_written' || item.incomplete)" class="card-ingest-status">
          <t-tag v-if="item.acquisition === 'model_written'" size="small" variant="light-outline" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</t-tag>
          <t-tag v-if="item.incomplete" size="small" theme="warning" variant="light-outline" :title="t('ingestStatus.incompleteHint')">{{ t('ingestStatus.incomplete') }}</t-tag>
        </div>

        <!-- Parse status display -->
        <div v-if="isParseInFlight(item.parse_status)" class="card-analyze card-analyze-trace">
          <t-icon name="loading" class="card-analyze-loading"></t-icon>
          <span
            class="card-analyze-txt card-analyze-trace-link"
            role="button"
            tabindex="0"
            :title="$t('knowledgeStages.viewTrace')"
            @click.stop="handleAction('view-trace', item)"
            @keydown.enter.stop="handleAction('view-trace', item)"
            @keydown.space.prevent.stop="handleAction('view-trace', item)"
          >{{ inFlightCardStatusText(item) }}</span>
          <button
            type="button"
            class="card-analyze-trace-btn"
            :title="$t('knowledgeStages.viewTrace')"
            :aria-label="$t('knowledgeStages.viewTrace')"
            @click.stop="handleAction('view-trace', item)"
          >
            <t-icon name="chart-line" />
          </button>
        </div>
        <div v-else-if="item.parse_status === 'failed'" class="card-analyze failure card-analyze-trace">
          <t-icon name="close-circle" class="card-analyze-loading failure"></t-icon>
          <span
            class="card-analyze-txt failure card-analyze-trace-link"
            role="button"
            tabindex="0"
            :title="$t('knowledgeStages.viewTrace')"
            @click.stop="handleAction('view-trace', item)"
            @keydown.enter.stop="handleAction('view-trace', item)"
            @keydown.space.prevent.stop="handleAction('view-trace', item)"
          >{{ $t('library.parsingFailed') }}</span>
          <button
            type="button"
            class="card-analyze-trace-btn"
            :title="$t('knowledgeStages.viewTrace')"
            :aria-label="$t('knowledgeStages.viewTrace')"
            @click.stop="handleAction('view-trace', item)"
          >
            <t-icon name="chart-bar" />
          </button>
        </div>
        <div v-else-if="item.parse_status === 'draft'" class="card-draft">
          <t-tag size="small" theme="warning" variant="light-outline">{{ $t('library.draft') }}</t-tag>
          <span class="card-draft-tip">{{ $t('library.draftTip') }}</span>
        </div>
        <div v-else-if="item.parse_status === 'rejected'" class="card-draft">
          <t-tag size="small" theme="warning" variant="light-outline">{{ $t('library.intakeWaiting') }}</t-tag>
        </div>
        <EntryClassification v-else-if="item.parse_status === 'completed'" :entry="item" compact @open-entity="emit('open-entity', $event)" />
      </div>

      <div class="card-bottom">

        <span class="card-time">{{ formatDocTime(item.updated_at) }}</span>
        <div class="card-bottom-right">

          <div class="card-type">
            <span>{{ getEntryType(item) }}</span>
          </div>
        </div>
      </div>
    </div>
    </div>
  </div>

  <DocumentMenuDelete ref="menuDelete" @deleted="emit('deleted', $event)" />

  <!-- Hover popover -->
  <Teleport to="body">
    <div
      v-show="hoveredCardItem"
      class="entry-card-hover-popover"
      :style="{ left: cardPopoverPos.x + 'px', top: cardPopoverPos.y + 'px' }"
    >
      <template v-if="hoveredCardItem">
        <div class="card-popover-title">{{ hoveredCardItem.file_name }}</div>
        <div v-if="isParseInFlight(hoveredCardItem.parse_status)" class="card-popover-status parsing">
          <EntryProcessingTimeline
            :entry-id="hoveredCardItem.id"
            :parse-status="hoveredCardItem.parse_status"
            :auto-poll="false"
            :compact="true"
          />
        </div>
        <div v-else-if="hoveredCardItem.parse_status === 'failed'" class="card-popover-status failure">
          <EntryProcessingTimeline
            :entry-id="hoveredCardItem.id"
            :parse-status="hoveredCardItem.parse_status"
            :auto-poll="false"
            :compact="true"
          />
        </div>
        <div v-else-if="hoveredCardItem.parse_status === 'draft'" class="card-popover-status draft">
          {{ $t('library.draft') }}
        </div>
        <template v-else>
          <div v-if="hoveredCardItem.description" class="card-popover-desc">
            {{ markdownToPlainText(hoveredCardItem.description, `[${t('ingestStatus.missingTypes.image')}]`) }}
          </div>
          <div v-if="(hoveredCardItem as any).source" class="card-popover-source" :title="(hoveredCardItem as any).source">
            <t-icon name="link" size="12px" /> {{ (hoveredCardItem as any).source }}
          </div>
          <div class="card-popover-extra">
            <span v-if="(hoveredCardItem as any).created_at" class="card-popover-created">
              {{ $t('library.createdAt') }}：{{ formatDocTime((hoveredCardItem as any).created_at) }}
            </span>
            <span v-if="formatFileSize((hoveredCardItem as any).file_size)" class="card-popover-size">
              {{ formatFileSize((hoveredCardItem as any).file_size) }}
            </span>
          </div>
        </template>
        <div class="card-popover-meta">
          <span class="card-popover-time">{{ $t('library.updatedAt') }}：{{ formatDocTime(hoveredCardItem.updated_at) }}</span>
          <span
            v-if="!(['mine', 'report'].includes(hoveredCardItem.source_kind || '')) && (hoveredCardItem as any).channel && (hoveredCardItem as any).channel !== 'web'"
            class="card-popover-channel"
          >{{ getChannelLabel((hoveredCardItem as any).channel) }}</span>

          <span class="card-popover-type">{{ getEntryType(hoveredCardItem) }}</span>
        </div>
        <div class="card-popover-hint">{{ $t('library.clickToViewFull') }}</div>
      </template>
    </div>
  </Teleport>
</template>

<style scoped lang="less">
@keyframes contentFadeIn {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}

.doc-card-view {
  width: 100%;
}

.doc-card-list {
  box-sizing: border-box;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 12px;
  align-content: flex-start;
  width: 100%;

  &.doc-card-list-animated {
    animation: contentFadeIn 0.32s ease-out;
  }
}

.entry-card {
  min-width: 240px;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--td-component-border);
  min-height: 180px;
  border-radius: 8px;
  overflow: hidden;
  box-sizing: border-box;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
  background: var(--td-bg-color-container);
  position: relative;
  cursor: pointer;
  transition: border-color 0.2s ease, box-shadow 0.2s ease, background-color 0.2s ease;

  &:hover {
    border-color: color-mix(in srgb, var(--td-component-stroke) 55%, var(--td-brand-color));
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.07);
  }

  .card-nav-check {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 29px;
    margin-right: 8px;
    cursor: pointer;

    .card-select-checkbox {
      margin: 0;
      line-height: 0;

      :deep(.t-checkbox) { align-items: center; }
      :deep(.t-checkbox__label) { display: none !important; width: 0 !important; min-width: 0 !important; margin: 0 !important; padding: 0 !important; }
      :deep(.t-checkbox__input) { margin: 0; }
      :deep(.t-checkbox__input-wrapper) { margin: 0; }
    }
  }

  .card-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 14px 8px;
  }

  .card-analyze {
    flex-shrink: 0;
    min-height: 52px;
    display: flex;
    align-items: flex-start;
  }

  .card-analyze-loading {
    display: block;
    color: var(--td-brand-color);
    font-size: 14px;
    margin-top: 2px;
  }

  .card-analyze-txt {
    color: var(--td-brand-color);
    font-family: var(--app-font-family);
    font-size: 11px;
    margin-left: 8px;
  }

  .card-analyze-trace {
    height: auto;
    min-height: 0;
    align-items: center;
    gap: 2px;
  }

  .card-analyze-trace-link {
    cursor: pointer;
    &:hover { text-decoration: underline; }
  }

  .card-analyze-trace-btn {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    margin: 0;
    padding: 2px;
    border: none;
    background: transparent;
    color: var(--td-brand-color);
    cursor: pointer;
    line-height: 1;
    border-radius: 4px;

    :deep(.t-icon) { font-size: 14px; }
    &:hover { background: var(--td-bg-color-component-hover); }
  }

  .card-analyze.failure .card-analyze-trace-btn { color: var(--td-error-color); }

  .failure { color: var(--td-error-color); }

  .card-content-nav {
    flex-shrink: 0;
    display: flex;
    align-items: flex-start;
    gap: 0;
    margin-bottom: 6px;
  }

  .card-content-title {
    flex: 1;
    min-width: 0;
    height: 24px;
    line-height: 24px;
    display: inline-block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--td-text-color-primary);
    font-family: var(--app-font-family);
    font-size: 14px;
    font-weight: 600;
    letter-spacing: 0.01em;
    margin-right: 8px;
  }

  .card-ingest-status { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin: 8px 0; }

  .more-wrap {
    flex-shrink: 0;
    display: flex;
    width: 25px;
    height: 25px;
    justify-content: center;
    align-items: center;
    border-radius: 5px;
    cursor: pointer;

    &:hover { background: var(--td-component-stroke); }
  }

  .more-icon { width: 14px; height: 14px; }
  .active-more { background: var(--td-component-stroke); }

  .card-content-txt {
    flex: 1;
    min-height: 0;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 12px;
    font-weight: 400;
    line-height: 19px;
  }

  .card-bottom {
    flex-shrink: 0;
    margin-top: auto;
    padding: 0 14px;
    box-sizing: border-box;
    height: 32px;
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: var(--td-bg-color-container);
    border-top: 1px solid var(--td-component-stroke);
  }

  .card-time {
    flex-shrink: 0;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-family);
    font-size: 12px;
    font-weight: 400;
    white-space: nowrap;
  }

  .card-type {
    flex-shrink: 0;
    color: var(--td-text-color-placeholder);
    font-family: var(--app-font-family);
    font-size: 11px;
    font-weight: 500;
    padding: 0;
    background: transparent;
    letter-spacing: 0.02em;
  }
}

.card-bottom-right {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  overflow: hidden;
}

// --- Card draft ---
.card-draft {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  flex-shrink: 0;
}

.card-draft-tip {
  color: var(--td-warning-color);
  font-size: 11px;
}

// --- Hover popover ---
.entry-card-hover-popover {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  min-width: 220px;
  max-width: 360px;
  padding: 12px 14px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
  font-family: var(--app-font-family);
  transition: opacity 0.15s ease;
  will-change: transform;
  backface-visibility: hidden;
  -webkit-backface-visibility: hidden;
  transform: translateZ(0);
  -webkit-transform: translateZ(0);

  .card-popover-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin-bottom: 8px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-popover-status {
    font-size: 12px;
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 6px;

    &.parsing { color: var(--td-brand-color); }
    &.failure { color: var(--td-error-color); }
    &.draft { color: var(--td-warning-color); }
  }

  .card-popover-desc {
    font-size: 12px;
    color: var(--td-text-color-secondary);
    line-height: 1.5;
    margin-bottom: 8px;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 5;
    line-clamp: 5;
    overflow: hidden;
  }

  .card-popover-source {
    font-size: 11px;
    color: var(--td-brand-color);
    margin-bottom: 6px;
    display: flex;
    align-items: center;
    gap: 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  .card-popover-extra {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    font-size: 11px;
    color: var(--td-text-color-secondary);
    margin-bottom: 6px;
  }

  .card-popover-created,
  .card-popover-size { flex-shrink: 0; }

  .card-popover-meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 11px;
    color: var(--td-text-color-secondary);
  }

  .card-popover-channel {
    padding: 1px 6px;
    background: var(--td-warning-color-light);
    color: var(--td-warning-color);
    border-radius: 4px;
  }

  .card-popover-type {
    padding: 1px 6px;
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-secondary);
    border-radius: 4px;
  }

  .card-popover-hint {
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--td-component-stroke);
    font-size: 11px;
    color: var(--td-text-color-secondary);
  }
}
</style>
