<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { write, type Question, type Answer } from '@/api/insights'
import EntryPicker from './EntryPicker.vue'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
const props = defineProps<{ setId: string; question?: Question; libraries: { id: string; name: string }[] }>()
const emit = defineEmits<{ saved: []; cancel: [] }>()
const { t } = useI18n()
const query = ref(props.question?.request.query || '')
const selectedLibraries = ref([...(props.question?.request.libraries || [])]), answers = ref<Answer[]>((props.question?.answers || []).map(a => ({ ...a })))
const collapse = ref(props.question?.request.collapse_similar || false)
const busy = ref(false), error = ref('')
const catalog = ref(props.libraries)
const effectiveLibraries = computed(() => selectedLibraries.value.length ? selectedLibraries.value : catalog.value.map(lib => lib.id))
const valid = computed(() => query.value.trim() && effectiveLibraries.value.length)
async function save() {
  if (!valid.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    const request = { ...props.question?.request, query: query.value.trim(), libraries: effectiveLibraries.value, collapse_similar: collapse.value }
    await write(`evaluation/sets/${props.setId}/questions${props.question ? `/${props.question.id}` : ''}`, { method: 'ordinary', request, answers: answers.value }, !!props.question)
    emit('saved')
  } catch (e: any) { error.value = e?.message || t('ikb19.saveFailed') }
  finally { busy.value = false }
}
</script>
<template>
  <form class="panel" @submit.prevent="save">
    <h2>{{ question ? t('ikb19.editQuestion') : t('ikb19.addQuestion') }}</h2>
    <p v-if="question?.answer_deleted" class="notice">{{ t('ikb19.answerDeleted') }}</p>
    <div class="fields">
      <label class="field">{{ t('ikb19.query') }}<textarea required :value="query" @input="query = ($event.target as HTMLTextAreaElement).value" /></label>
    </div>
    <div class="scope-field"><p>{{ t('ikb19.libraries') }}</p><LibraryScopeSelect :model-value="selectedLibraries" :disabled="busy" @update:model-value="selectedLibraries = $event" @loaded="catalog = $event" /></div>
    <label><input type="checkbox" :checked="collapse" @change="collapse = ($event.target as HTMLInputElement).checked" /> {{ t('ikb19.collapse') }}</label>
    <h3>{{ t('ikb19.answers') }}</h3>
    <EntryPicker :libraries="effectiveLibraries" :model-value="answers" grades @update:model-value="answers = $event" />
    <p class="muted">{{ t('ikb19.gradeHint') }}</p>
    <p v-if="error" class="notice error" role="alert">{{ error }} {{ t('ikb19.reviseRetry') }}</p>
    <div class="actions"><button class="primary" type="submit" :disabled="busy || !valid">{{ busy ? t('ikb19.saving') : t('ikb19.save') }}</button><button type="button" :disabled="busy" @click="emit('cancel')">{{ t('ikb19.cancel') }}</button></div>
  </form>
</template>
<style scoped>.scope-field { margin: 16px 0; min-width: 0; }</style>
