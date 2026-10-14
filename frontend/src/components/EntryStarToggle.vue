<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { setEntryStar, type EntryJudgment } from '@/api/entry-judgments'

const props = defineProps<{ entry: EntryJudgment; disabled?: boolean }>()
const emit = defineEmits<{ updated: [value: EntryJudgment] }>()
const { t } = useI18n()
const saving = ref(false), error = ref('')
async function toggle() {
  if (saving.value || props.disabled) return
  const id = props.entry.id, starred = !props.entry.starred
  saving.value = true
  error.value = ''
  try {
    const response = await setEntryStar(id, starred)
    if (!response.success) throw new Error(response.message)
    if (props.entry.id !== id) return
    emit('updated', { ...response.data, id, starred })
    MessagePlugin.success(t(starred ? 'articleJudgments.starred' : 'articleJudgments.unstarred'))
  } catch { if (props.entry.id === id) error.value = t('articleJudgments.starFailed') }
  finally { saving.value = false }
}
</script>

<template>
  <div class="entry-star-control" @click.stop>
    <t-button size="small" variant="text" :theme="entry.starred ? 'primary' : 'default'" :loading="saving" :disabled="disabled || saving" :aria-pressed="Boolean(entry.starred)" :aria-label="t(entry.starred ? 'articleJudgments.unstar' : 'articleJudgments.star')" @click="toggle">
      <template #icon><t-icon :name="entry.starred ? 'star-filled' : 'star'" /></template>
    </t-button>
    <span v-if="error" class="star-error" role="alert">{{ error }}</span>
  </div>
</template>

<style scoped lang="less">
.entry-star-control { display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0; }
.star-error { color: var(--td-error-color); font-size: 12px; overflow-wrap: anywhere; }
</style>
