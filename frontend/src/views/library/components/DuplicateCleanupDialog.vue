<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { MessagePlugin } from 'tdesign-vue-next';
import { batchDeleteEntry, listLibraryDuplicates, type DuplicateEntry, type DuplicateGroup, type LibraryCatalogItem } from '@/api/library';

const props = defineProps<{ libraries: LibraryCatalogItem[]; defaultLibraryId?: string }>();
const visible = defineModel<boolean>('visible', { required: true });
const emit = defineEmits<{ deleted: [ids: string[]] }>();
const { t, locale } = useI18n();
const libraryId = ref(''), groups = ref<DuplicateGroup[] | null>(null), loading = ref(false), deleting = ref(false), error = ref('');
const options = computed(() => props.libraries.map(item => ({ value: item.id, label: item.display_name || item.name })));
const removeIds = computed(() => (groups.value || []).flatMap(group => group.remove.map(item => item.id)));
// Matches the per-request limit of POST /entries/batch-delete.
const BATCH = 200;

watch(visible, open => {
  if (!open) return;
  libraryId.value = props.defaultLibraryId || props.libraries[0]?.id || '';
  groups.value = null; error.value = '';
});
watch(libraryId, () => { groups.value = null; error.value = ''; });

async function find() {
  if (!libraryId.value || loading.value) return;
  loading.value = true; error.value = '';
  try {
    const result = await listLibraryDuplicates(libraryId.value);
    if (!result.success) throw new Error(result.message);
    groups.value = result.data || [];
  } catch (cause: any) {
    error.value = cause?.message || t('duplicateCleanup.loadFailed');
  } finally { loading.value = false; }
}

async function remove() {
  if (!removeIds.value.length || deleting.value) return;
  deleting.value = true; error.value = '';
  try {
    for (let i = 0; i < removeIds.value.length; i += BATCH) {
      const result: any = await batchDeleteEntry(removeIds.value.slice(i, i + BATCH));
      if (!result?.success) throw new Error(result?.message);
    }
    const ids = removeIds.value;
    MessagePlugin.success(t('duplicateCleanup.deleted', { count: ids.length }));
    visible.value = false;
    emit('deleted', ids);
  } catch (cause: any) {
    error.value = cause?.message || t('duplicateCleanup.deleteFailed');
  } finally { deleting.value = false; }
}

const label = (item: DuplicateEntry) => item.title || item.id;
const when = (item: DuplicateEntry) => item.published_at
  ? t('duplicateCleanup.published', { date: item.published_at })
  : t('duplicateCleanup.ingested', { date: new Date(item.created_at).toLocaleString(locale.value) });
</script>

<template>
  <t-dialog v-model:visible="visible" attach="body" :header="t('duplicateCleanup.title')" width="720px" placement="center" :footer="false" destroy-on-close>
    <p class="dup-hint">{{ t('duplicateCleanup.hint') }}</p>
    <div class="dup-toolbar">
      <t-select v-model="libraryId" :options="options" :aria-label="t('duplicateCleanup.library')" class="dup-library" />
      <t-button :loading="loading" :disabled="!libraryId || deleting" @click="find">{{ t('duplicateCleanup.find') }}</t-button>
    </div>
    <p v-if="error" role="alert" class="dup-error">{{ error }}</p>
    <template v-if="groups">
      <p v-if="!groups.length">{{ t('duplicateCleanup.none') }}</p>
      <template v-else>
        <p>{{ t('duplicateCleanup.summary', { groups: groups.length, count: removeIds.length }) }}</p>
        <ol class="dup-groups">
          <li v-for="group in groups" :key="group.keep.id">
            <div><t-tag theme="success" variant="light" size="small">{{ t('duplicateCleanup.keep') }}</t-tag> {{ label(group.keep) }} <small>{{ when(group.keep) }}</small></div>
            <div v-for="item in group.remove" :key="item.id" class="dup-remove">
              <t-tag theme="danger" variant="light" size="small">{{ t('duplicateCleanup.remove') }}</t-tag> {{ label(item) }} <small>{{ when(item) }}</small>
              <small v-if="item.original_url" class="dup-url">{{ item.original_url }}</small>
            </div>
          </li>
        </ol>
        <div class="dup-actions">
          <t-button variant="outline" :disabled="deleting" @click="visible = false">{{ t('common.cancel') }}</t-button>
          <t-button theme="danger" :loading="deleting" @click="remove">{{ t('duplicateCleanup.confirm', { count: removeIds.length }) }}</t-button>
        </div>
      </template>
    </template>
  </t-dialog>
</template>

<style scoped>
.dup-hint { color: var(--td-text-color-secondary); margin: 0 0 12px; }
.dup-toolbar { display: flex; gap: 8px; margin-bottom: 12px; }
.dup-library { flex: 1; min-width: 0; }
.dup-error { color: var(--td-error-color); }
.dup-groups { max-height: 40vh; overflow: auto; padding-left: 20px; display: grid; gap: 10px; }
.dup-remove { margin-top: 4px; }
.dup-url { display: block; color: var(--td-text-color-placeholder); overflow-wrap: anywhere; }
.dup-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
</style>
