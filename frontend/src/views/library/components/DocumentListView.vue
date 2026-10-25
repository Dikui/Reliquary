<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { formatFileSize, getFileIcon } from '@/utils/files';
import { entryFileFormatLabel, entryChannelLabel } from '@/utils/entrySourceLabels';
import DocumentActionMenu from './DocumentActionMenu.vue';
import DocumentMenuDelete from './DocumentMenuDelete.vue';
import EntryUpdateNotice from '@/components/EntryUpdateNotice.vue';
import EntryClassification from '@/components/EntryClassification.vue';
import EntryProcessingStatus from '@/components/EntryProcessingStatus.vue';
import EntryStarToggle from '@/components/EntryStarToggle.vue';
import SelectionCheckbox from '@/components/SelectionCheckbox.vue';
import type { EntryJudgment } from '@/api/entry-judgments';
import type { ClassifiedEntry } from '@/types/entryClassification';

interface EntryItem extends ClassifiedEntry {
  id: string;
  file_name: string;
  file_type?: string;
  file_size?: number | string;
  type?: string;
  parse_status?: string;
  updated_at?: string;
  source?: string;
  description?: string;
  channel?: string;
  isMore?: boolean;
}

const props = defineProps<{
  items: EntryItem[];
  selectedIds: Set<string>;
  traceVisibleIds: Record<string, boolean>;
  loading?: boolean;
  deleteEntry?: (item: EntryItem) => Promise<boolean>;
}>();

const emit = defineEmits<{
  (e: 'open', item: EntryItem): void;
  (e: 'open-entity', id: string): void;
  (e: 'judgment-updated', value: EntryJudgment): void;
  (e: 'toggle-row', id: string, checked: boolean, shiftKey: boolean): void;
  (e: 'toggle-all', checked: boolean): void;
  (e: 'action', action: 'download' | 'reparse' | 'cancel-parse' | 'delete' | 'view-trace' | 'batch-manage', item: EntryItem): void;
  (e: 'probe-trace', item: EntryItem): void;
  (e: 'deleted', id: string): void;
  (e: 'processing-updated', value: Record<string, unknown> & { id: string }): void;
}>();

const { t } = useI18n();
const menuDelete = ref<InstanceType<typeof DocumentMenuDelete> | null>(null);

const formatTime = (time?: string) => {
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

const getSourceInfo = (item: EntryItem) => ({ icon: 'link', label: entryChannelLabel(item, t) });

const allSelected = computed(() => {
  return props.items.length > 0 && props.items.every(i => props.selectedIds.has(i.id));
});
const someSelected = computed(() => {
  return props.items.some(i => props.selectedIds.has(i.id)) && !allSelected.value;
});

const onHeaderCheckboxChange = (checked: boolean) => {
  emit('toggle-all', checked);
};

const onRowCheckboxChange = (item: EntryItem, checked: boolean, ctx?: { e?: Event }) => {
  const me = ctx?.e as MouseEvent | undefined;
  emit('toggle-row', item.id, checked, !!me?.shiftKey);
};

const moreOpen = ref<string | null>(null);
const onMoreVisible = (id: string, visible: boolean) => {
  moreOpen.value = visible ? id : null;
  if (visible) {
    const it = props.items.find(i => i.id === id);
    if (it) emit('probe-trace', it);
  } else {
  }
};

// 吸顶检测：哨兵离开视口说明 header 已吸附在滚动容器顶部
const stickySentinel = ref<HTMLElement | null>(null);
const headerStuck = ref(false);
let stickyObserver: IntersectionObserver | null = null;
onMounted(() => {
  if (!stickySentinel.value || typeof IntersectionObserver === 'undefined') return;
  stickyObserver = new IntersectionObserver(
    (entries) => {
      headerStuck.value = !entries[0].isIntersecting;
    },
    { threshold: 0 },
  );
  stickyObserver.observe(stickySentinel.value);
});
onBeforeUnmount(() => {
  stickyObserver?.disconnect();
  stickyObserver = null;
});

const handleAction = (action: 'download' | 'reparse' | 'cancel-parse' | 'delete' | 'view-trace' | 'batch-manage', item: EntryItem) => {
  moreOpen.value = null;
  item.isMore = false;
  emit('action', action, item);
};

const openDelete = async (item: EntryItem) => {
  item.isMore = false;
  moreOpen.value = null;
  await nextTick();
  menuDelete.value?.open(item, props.deleteEntry ? () => props.deleteEntry!(item) : undefined);
};

</script>

<template>
  <div class="doc-list-view" :class="{ 'is-loading': loading }">
    <div ref="stickySentinel" class="doc-list-sticky-sentinel" aria-hidden="true"></div>
    <div class="doc-list-header" :class="{ 'is-stuck': headerStuck }" role="row">
      <div class="cell cell-check" role="columnheader" @click.stop>
        <t-checkbox class="doc-list-check" size="small" :checked="allSelected" :indeterminate="someSelected"
          :disabled="!items.length" :title="t('library.selectAll')" @change="onHeaderCheckboxChange" />
      </div>
      <div class="cell cell-name" role="columnheader">{{ t('library.columnName') }}</div>

      <div class="cell cell-source" role="columnheader">{{ t('library.columnSource') }}</div>
      <div class="cell cell-size" role="columnheader">{{ t('library.columnSize') }}</div>
      <div class="cell cell-status" role="columnheader">{{ t('library.columnStatus') }}</div>
      <div class="cell cell-time" role="columnheader">{{ t('library.columnUpdatedAt') }}</div>
      <div class="cell cell-actions" role="columnheader"></div>
    </div>

    <div class="doc-list-body">

      <div v-for="item in items" :key="item.id" class="doc-list-row"
        :class="{ selected: selectedIds.has(item.id), 'menu-open': moreOpen === item.id }" :data-select-id="item.id"
        role="row" @click="emit('open', item)">
        <div class="cell cell-check" @click.stop>
          <SelectionCheckbox class="doc-list-check" size="small" :checked="selectedIds.has(item.id)" :title="item.file_name"
            @change="(c: boolean, ctx?: { e?: Event }) => onRowCheckboxChange(item, c, ctx)" />
        </div>

        <div class="cell cell-name">
          <span class="row-file-icon-wrap">
            <t-icon :name="getFileIcon(item)" />
          </span>
          <div class="row-file-text">
            <span class="row-file-name" :title="item.file_name">{{ item.file_name }}</span>
            <span class="row-muted">{{ entryFileFormatLabel(item, t) }}</span>

            <EntryClassification :entry="item" compact @open-entity="emit('open-entity', $event)" />
        <EntryUpdateNotice :has-update="item.has_update" :latest-entry-id="item.latest_entry_id" />
          </div>
        </div>

        <div class="cell cell-source">
          <t-icon class="row-source-icon" :name="getSourceInfo(item).icon" />
          <span class="row-source-label">{{ getSourceInfo(item).label }}</span>
        </div>

        <div class="cell cell-size">
          <span class="row-mono">{{ formatFileSize(item.file_size) || '--' }}</span>
        </div>

        <div class="cell cell-status">
          <EntryProcessingStatus :entry="item" compact @updated="emit('processing-updated', $event)" />
        </div>

        <div class="cell cell-time">
          <span class="row-mono">{{ formatTime(item.updated_at) }}</span>
        </div>

        <div class="cell cell-actions" @click.stop>
          <EntryStarToggle v-if="item.parse_status === 'completed'" :entry="item" @updated="emit('judgment-updated', $event)" />
          <t-popup v-model="item.isMore" placement="bottom-right" trigger="click" :destroy-on-close="false" overlay-class-name="card-more"
            :on-visible-change="(v: boolean) => onMoreVisible(item.id, v)">
            <button class="row-more-btn" :class="{ active: moreOpen === item.id }" type="button"
              :aria-label="t('library.columnActions')">
              <t-icon name="more" size="16px" />
            </button>
            <template #content>
              <!-- Normal menu -->
              <div  class="card-menu">
                <DocumentActionMenu
                  :item="item"
                  :trace-visible="!!traceVisibleIds[item.id] || (item.parse_status === 'pending' || item.parse_status === 'processing' || item.parse_status === 'finalizing')"
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
      </div>
    </div>
    <DocumentMenuDelete ref="menuDelete" @deleted="emit('deleted', $event)" />
  </div>
</template>

<style scoped lang="less">
@keyframes doc-list-fade-in {
  from {
    opacity: 0;
    transform: translateY(6px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.doc-list-view {
  display: flex;
  flex-direction: column;
  width: 100%;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 9px;
  /* 不能用 overflow:hidden，否则表头 position:sticky 相对外层滚动区失效 */
  overflow: visible;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
  animation: doc-list-fade-in 0.32s ease-out;
  container-type: inline-size;
}

.doc-list-header,
.doc-list-row {
  display: grid;
  grid-template-columns:
    44px // checkbox
    minmax(260px, 2.6fr) // name
    minmax(96px, 0.8fr) // source
    96px // size
    minmax(96px, 0.7fr) // status
    140px // updated_at
    80px; // actions
  align-items: center;
  column-gap: 0;
  padding: 0 16px;
}

.doc-list-sticky-sentinel {
  height: 0;
  margin: 0;
  padding: 0;
  border: 0;
  pointer-events: none;
}

.doc-list-header {
  position: sticky;
  top: 0;
  z-index: 3;
  height: 40px;
  font-size: 12px;
  font-weight: 500;
  font-family: var(--app-font-family);
  color: var(--td-text-color-secondary);
  background: var(--td-bg-color-secondarycontainer);
  border-bottom: 1px solid var(--td-component-stroke);
  border-radius: 8px 8px 0 0;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
  transition: border-radius 0.15s ease, box-shadow 0.2s ease;

  &.is-stuck {
    border-radius: 0;
    box-shadow: 0 4px 10px rgba(0, 0, 0, 0.08);
  }
}

.doc-list-body {
  display: flex;
  flex-direction: column;
  border-radius: 0 0 8px 8px;
  overflow: hidden;
}

.doc-list-row {
  position: relative;
  min-height: 60px;
  font-size: 13px;
  color: var(--td-text-color-primary);
  border-bottom: 1px solid var(--td-component-stroke);
  cursor: pointer;
  transition: background-color 0.2s ease, box-shadow 0.2s ease, border-color 0.2s ease;

  &:last-child {
    border-bottom: 0;
  }

  &:hover:not(.selected),
  &.menu-open:not(.selected) {
    background: var(--td-bg-color-secondarycontainer);
  }

  &:hover .row-more-btn,
  &.menu-open .row-more-btn,
  &.selected .row-more-btn {
    opacity: 1;
  }
}

.cell {
  display: flex;
  align-items: center;
  min-width: 0;
  padding: 0 8px;

  &:first-child {
    padding-left: 0;
  }

  &:last-child {
    padding-right: 0;
  }
}

.cell-check {
  justify-content: center;
  padding: 0;
}

.cell-name {
  gap: 10px;
  font-family: var(--app-font-family);
}

.cell-size,
.cell-time {
  justify-content: flex-end;
}

.cell-actions {
  justify-content: flex-end;
  gap: 4px;
}

/* TDesign 勾选框：去掉空白 label、与表格行对齐 */
.doc-list-check {
  margin: 0;

  :deep(.t-checkbox) {
    align-items: center;
  }

  :deep(.t-checkbox__label) {
    display: none !important;
    width: 0 !important;
    min-width: 0 !important;
    margin: 0 !important;
    padding: 0 !important;
  }

  :deep(.t-checkbox__input) {
    margin: 0;
  }

  :deep(.t-checkbox__input-wrapper) {
    margin: 0;
  }
}

.row-file-icon-wrap {
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
}

.row-file-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.row-file-name {
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--td-text-color-primary);
}

.row-file-desc {
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
}

.cell-source {
  gap: 6px;
  min-width: 0;
}

.row-source-icon {
  flex-shrink: 0;
  font-size: 14px;
  color: var(--td-text-color-secondary);
}

.row-source-label {
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}

.row-muted {
  color: var(--td-text-color-disabled, #bbb);
}

.row-mono {
  font-variant-numeric: tabular-nums;
  font-size: 12px;
  font-family: var(--app-font-family);
  color: var(--td-text-color-secondary);
}

.row-status-tag :deep(.t-icon) {
  margin-right: 2px;
}

.icon-spin {
  animation: doc-list-spin 0.9s linear infinite;
}

@keyframes doc-list-spin {
  to {
    transform: rotate(360deg);
  }
}

.row-more-btn {
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  background: transparent;
  border-radius: 5px;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s ease, background-color 0.15s ease, color 0.15s ease;

  &:hover {
    background: var(--td-component-stroke);
    color: var(--td-text-color-primary);
  }

  &.active {
    opacity: 1;
    background: var(--td-component-stroke);
    color: var(--td-text-color-primary);
  }
}

/* 列宽之和约 850px；容器变窄时先隐藏来源和大小两列 */
@container (max-width: 860px) {
  .doc-list-header,
  .doc-list-row { grid-template-columns: 44px minmax(0, 2.6fr) minmax(96px, 0.7fr) 140px 80px; }
  .cell-source, .cell-size { display: none; }
}

@media (max-width: 640px) {
  .doc-list-header { display: none; }
  .doc-list-row { grid-template-columns: 32px minmax(0, 1fr) 52px; padding: 12px 6px; }
  .doc-list-row > .cell-source, .doc-list-row > .cell-size, .doc-list-row > .cell-time { display: none; }
  .doc-list-row > .cell-check { grid-area: 1 / 1; }
  .doc-list-row > .cell-name { grid-area: 1 / 2; }
  .doc-list-row > .cell-actions { grid-area: 1 / 3; }
  .doc-list-row > .cell-status { grid-area: 2 / 2 / 3 / 4; padding-top: 8px; }
  .doc-list-row .row-more-btn { opacity: 1; }
  .doc-list-row .row-file-name { white-space: normal; overflow-wrap: anywhere; }
  .doc-list-view { min-width: 0; overflow-x: hidden; }
}
</style>
