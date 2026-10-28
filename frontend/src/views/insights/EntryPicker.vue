<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
import { read, type Answer } from '@/api/insights'
const props = defineProps<{ libraries: string[]; modelValue: Answer[]; grades?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [Answer[]] }>()
const { t } = useI18n()
const shortId = (id: string) => id.length > 12 ? id.slice(0, 8) : id
const keyword = ref(''), entries = ref<{ id: string; title: string; library_id: string }[]>([])
const busy = ref(false), error = ref(''), searched = ref(false), offset = ref(0), hasMore = ref(false)
const titles = ref<Record<string, string>>({})
let request = 0
watch(() => props.libraries.join(','), () => { request++; entries.value = []; searched.value = false; busy.value = false })
async function search(more = false) {
  if (!keyword.value.trim() || !props.libraries.length) return
  const ticket = ++request
  busy.value = true; error.value = ''; searched.value = true
  const start = more ? offset.value + 50 : 0
  try {
    const data = await read<{ id: string; title: string; library_id: string }[]>('entries/search', { keyword: keyword.value, limit: 50, offset: start })
    if (ticket !== request) return
    offset.value = start; hasMore.value = data.length === 50
    entries.value = more ? [...entries.value, ...data.filter(e => props.libraries.includes(e.library_id))] : data.filter(e => props.libraries.includes(e.library_id))
    for (const entry of entries.value) titles.value[entry.id] = entry.title
  } catch { if (ticket === request) error.value = t('ikb19.searchFailed') }
  finally { if (ticket === request) busy.value = false }
}
function toggle(id: string) {
  emit('update:modelValue', props.modelValue.some(a => a.entry_id === id) ? props.modelValue.filter(a => a.entry_id !== id) : [...props.modelValue, { entry_id: id, grade: 1 }])
}
function grade(id: string, value: string) {
  emit('update:modelValue', props.modelValue.map(a => a.entry_id === id ? { ...a, grade: Math.max(0, Math.min(10, Number(value) || 0)) } : a))
}
</script>
<template>
  <div class="entry-picker">
    <div class="toolbar">
      <label class="field">{{ t('ikb19.searchEntries') }}<input class="rq-control" :value="keyword" @input="keyword = ($event.target as HTMLInputElement).value" @keydown.enter.prevent="search()" /></label>
      <Button type="button" :disabled="busy || !keyword.trim() || !libraries.length" @click="search()">{{ t('ikb19.search') }}</Button>
    </div>
    <p class="muted">{{ t('ikb19.pickerHint') }}</p>
    <p v-if="error" role="alert" class="notice error">{{ error }} <Button type="button" @click="search()">{{ t('ikb19.retry') }}</Button></p>
    <p v-if="busy" role="status">{{ t('ikb19.loading') }}</p>
    <p v-else-if="searched && !entries.length" class="empty">{{ t('ikb19.noSearchResults') }}</p>
    <div v-for="entry in entries" :key="entry.id">
      <label><input class="rq-checkbox" type="checkbox" :checked="modelValue.some(a => a.entry_id === entry.id)" @change="toggle(entry.id)" /> {{ entry.title }} <small :title="entry.id">{{ shortId(entry.id) }}</small></label>
    </div>
    <Button v-if="hasMore" type="button" :disabled="busy" @click="search(true)">{{ t('ikb19.more') }}</Button>
    <ul v-if="modelValue.length">
      <li v-for="answer in modelValue" :key="answer.entry_id">
        <a :title="answer.entry_id" :href="`/platform/entries/${encodeURIComponent(answer.entry_id)}`">{{ titles[answer.entry_id] || shortId(answer.entry_id) }}</a>
        <label v-if="grades"> {{ t('ikb19.grade') }} <input class="rq-control" type="number" min="0" max="10" :aria-label="`${t('ikb19.grade')} ${answer.entry_id}`" :value="answer.grade" @input="grade(answer.entry_id, ($event.target as HTMLInputElement).value)" /></label>
        <Button type="button" :aria-label="`${t('ikb19.remove')} ${answer.entry_id}`" @click="toggle(answer.entry_id)">{{ t('ikb19.remove') }}</Button>
      </li>
    </ul>
  </div>
</template>
