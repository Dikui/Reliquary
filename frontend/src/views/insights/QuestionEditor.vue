<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { write, type Question, type Answer } from '@/api/insights'
import EntryPicker from './EntryPicker.vue'
const props = defineProps<{ setId: string; question?: Question; libraries: { id: string; name: string }[] }>()
const emit = defineEmits<{ saved: []; cancel: [] }>()
const { t } = useI18n()
const query = ref(props.question?.request.query || ''), method = ref(props.question?.method || 'ordinary')
const selectedLibraries = ref([...(props.question?.request.libraries || [])]), answers = ref<Answer[]>((props.question?.answers || []).map(a => ({ ...a })))
const graph = ref(JSON.stringify(props.question?.request.graph_parameters || {}, null, 2)), collapse = ref(props.question?.request.collapse_similar || false)
const busy = ref(false), error = ref('')
const valid = computed(() => query.value.trim() && selectedLibraries.value.length)
function toggleLibrary(id: string) { selectedLibraries.value = selectedLibraries.value.includes(id) ? selectedLibraries.value.filter(v => v !== id) : [...selectedLibraries.value, id] }
async function save() {
  if (!valid.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    const request = { ...props.question?.request, query: query.value.trim(), libraries: selectedLibraries.value, collapse_similar: collapse.value }
    if (method.value === 'graph') request.graph_parameters = JSON.parse(graph.value)
    else delete request.graph_parameters
    await write(`evaluation/sets/${props.setId}/questions${props.question ? `/${props.question.id}` : ''}`, { method: method.value, request, answers: answers.value }, !!props.question)
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
      <label class="field">{{ t('ikb19.method') }}<select :value="method" @change="method = ($event.target as HTMLSelectElement).value"><option value="ordinary">{{ t('ikb19.ordinary') }}</option><option value="graph">{{ t('ikb19.graph') }}</option></select></label>
    </div>
    <fieldset><legend>{{ t('ikb19.libraries') }}</legend><label v-for="lib in libraries" :key="lib.id"><input type="checkbox" :checked="selectedLibraries.includes(lib.id)" @change="toggleLibrary(lib.id)" /> {{ lib.name }} </label></fieldset>
    <label><input type="checkbox" :checked="collapse" @change="collapse = ($event.target as HTMLInputElement).checked" /> {{ t('ikb19.collapse') }}</label>
    <label v-if="method === 'graph'" class="field">{{ t('ikb19.graphParameters') }}<textarea :value="graph" @input="graph = ($event.target as HTMLTextAreaElement).value" /></label>
    <h3>{{ t('ikb19.answers') }}</h3>
    <EntryPicker :libraries="selectedLibraries" :model-value="answers" grades @update:model-value="answers = $event" />
    <p class="muted">{{ t('ikb19.gradeHint') }}</p>
    <p v-if="error" class="notice error" role="alert">{{ error }} {{ t('ikb19.reviseRetry') }}</p>
    <div class="actions"><button class="primary" type="submit" :disabled="busy || !valid">{{ busy ? t('ikb19.saving') : t('ikb19.save') }}</button><button type="button" :disabled="busy" @click="emit('cancel')">{{ t('ikb19.cancel') }}</button></div>
  </form>
</template>
<style scoped>fieldset { border: 1px solid var(--td-component-stroke); border-radius: 6px; margin: 16px 0; } fieldset label { display: inline-block; padding: 6px; }</style>
