<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
import { write, type Question, type Answer } from '@/api/insights'
import EntryPicker from './EntryPicker.vue'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
const props = defineProps<{ setId: string; question?: Question; libraries: { id: string; name: string }[] }>()
const emit = defineEmits<{ saved: []; cancel: []; busy: [value: boolean] }>()
const { t } = useI18n()
const query = ref(props.question?.request.query || ''), method = ref(props.question?.method || 'ordinary')
const selectedLibraries = ref([...(props.question?.request.libraries || [])]), answers = ref<Answer[]>((props.question?.answers || []).map(a => ({ ...a })))
const graph = ref(JSON.stringify(props.question?.request.graph_parameters || {}, null, 2)), collapse = ref(props.question?.request.collapse_similar || false)
const busy = ref(false), error = ref('')
const catalog = ref(props.libraries)
const effectiveLibraries = computed(() => selectedLibraries.value.length ? selectedLibraries.value : catalog.value.map(lib => lib.id))
const valid = computed(() => query.value.trim() && effectiveLibraries.value.length)
async function save() {
  if (!valid.value || busy.value) return
  busy.value = true; emit('busy', true); error.value = ''
  try {
    const request = { ...props.question?.request, query: query.value.trim(), libraries: effectiveLibraries.value, collapse_similar: collapse.value }
    if (method.value === 'graph') {
      try { request.graph_parameters = JSON.parse(graph.value) } catch { throw new Error(t('diagnosticsB.invalidGraph')) }
    }
    else delete request.graph_parameters
    await write(`evaluation/sets/${props.setId}/questions${props.question ? `/${props.question.id}` : ''}`, { method: method.value, request, answers: answers.value }, !!props.question)
    emit('saved')
  } catch (e: any) { error.value = e?.message || t('ikb19.saveFailed') }
  finally { busy.value = false; emit('busy', false) }
}
</script>
<template>
  <form class="dialog-form" @submit.prevent="save">
    <p v-if="question?.answer_deleted" class="notice">{{ t('ikb19.answerDeleted') }}</p>
    <div class="fields">
      <label class="field">{{ t('ikb19.query') }}<textarea class="rq-control" required :value="query" @input="query = ($event.target as HTMLTextAreaElement).value" /></label>
      <label class="field">{{ t('ikb19.method') }}<select class="rq-control" :value="method" @change="method = ($event.target as HTMLSelectElement).value"><option value="ordinary">{{ t('ikb19.ordinary') }}</option><option value="graph">{{ t('ikb19.graph') }}</option></select></label>
    </div>
    <div class="scope-field"><p>{{ t('ikb19.libraries') }}</p><LibraryScopeSelect :model-value="selectedLibraries" :disabled="busy" @update:model-value="selectedLibraries = $event" @loaded="catalog = $event" /></div>
    <label><input class="rq-checkbox" type="checkbox" :checked="collapse" @change="collapse = ($event.target as HTMLInputElement).checked" /> {{ t('ikb19.collapse') }}</label>
    <label v-if="method === 'graph'" class="field">{{ t('ikb19.graphParameters') }}<textarea class="rq-control" :value="graph" @input="graph = ($event.target as HTMLTextAreaElement).value" /></label>
    <h3>{{ t('ikb19.answers') }}</h3>
    <EntryPicker :libraries="effectiveLibraries" :model-value="answers" grades @update:model-value="answers = $event" />
    <p class="muted">{{ t('ikb19.gradeHint') }}</p>
    <p v-if="error" class="notice error" role="alert">{{ error }} {{ t('ikb19.reviseRetry') }}</p>
    <div class="actions"><Button variant="primary" type="submit" :disabled="busy || !valid">{{ busy ? t('ikb19.saving') : t('ikb19.save') }}</Button><Button type="button" :disabled="busy" @click="emit('cancel')">{{ t('ikb19.cancel') }}</Button></div>
  </form>
</template>
