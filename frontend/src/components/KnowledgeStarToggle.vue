<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { setKnowledgeStar, type ArticleJudgment } from '@/api/knowledge-judgments'

const props = defineProps<{ knowledge: ArticleJudgment; disabled?: boolean }>()
const emit = defineEmits<{ updated: [value: ArticleJudgment] }>()
const { t } = useI18n()
const saving = ref(false), error = ref('')
async function toggle() {
  if (saving.value || props.disabled) return
  const id = props.knowledge.id, starred = !props.knowledge.starred
  saving.value = true
  error.value = ''
  try {
    const response = await setKnowledgeStar(id, starred)
    if (!response.success) throw new Error(response.message)
    if (props.knowledge.id !== id) return
    emit('updated', { ...response.data, id, starred })
    MessagePlugin.success(t(starred ? 'articleJudgments.starred' : 'articleJudgments.unstarred'))
  } catch { if (props.knowledge.id === id) error.value = t('articleJudgments.starFailed') }
  finally { saving.value = false }
}
</script>

<template>
  <div class="knowledge-star-control" @click.stop>
    <t-button size="small" variant="text" :theme="knowledge.starred ? 'primary' : 'default'" :loading="saving" :disabled="disabled || saving" :aria-pressed="Boolean(knowledge.starred)" :aria-label="t(knowledge.starred ? 'articleJudgments.unstar' : 'articleJudgments.star')" @click="toggle">
      <template #icon><t-icon :name="knowledge.starred ? 'star-filled' : 'star'" /></template>
    </t-button>
    <span v-if="error" class="star-error" role="alert">{{ error }}</span>
  </div>
</template>

<style scoped lang="less">
.knowledge-star-control { display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0; }
.star-error { color: var(--td-error-color); font-size: 12px; overflow-wrap: anywhere; }
</style>
