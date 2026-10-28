<script setup lang="ts">
import { computed, onMounted, ref, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
const props = withDefaults(defineProps<{ modelValue: string[]; disabled?: boolean; single?: boolean }>(), { disabled: false })
const emit = defineEmits<{ 'update:modelValue': [ids: string[]]; loaded: [libraries: LibraryCatalogItem[]] }>()
const { t, locale } = useI18n()
const libraries = ref<LibraryCatalogItem[]>([]), loading = ref(false), error = ref('')
let sequence = 0
const options = computed(() => libraries.value.map(item => ({ value: item.id, label: item.display_name || item.name })))
async function load() {
  const current = ++sequence; loading.value = true; error.value = ''
  try { const response = await listLibraries(); if (!response.success) throw new Error(response.message); if (current === sequence) { libraries.value = response.data || []; if (props.single && !libraries.value.some(lib => lib.id === props.modelValue[0])) emit('update:modelValue', libraries.value[0] ? [libraries.value[0].id] : []); emit('loaded', libraries.value) } }
  catch (cause: any) { if (current === sequence) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === sequence) loading.value = false }
}
watch(locale, load)
onMounted(load)
onBeforeUnmount(() => { sequence++ })
</script>
<template>
  <div class="library-scope">
    <t-select :value="single ? props.modelValue[0] : props.modelValue" :disabled="disabled" :multiple="!single" :clearable="!single" filterable :options="options" :loading="loading" :placeholder="t(single ? 'ikb18.scope' : 'ikb18.all')" :aria-label="t('ikb18.scope')" @change="emit('update:modelValue', single ? [$event as string] : $event as string[])" />
    <p v-if="error" role="alert">{{ error }} <t-button variant="text" @click="load">{{ t('common.retry') }}</t-button></p>
  </div>
</template>
<style scoped>
.library-scope { min-width: 0; width: 100%; max-width: 440px; }
.library-scope p { color: var(--td-error-color); font-size: 12px; }
</style>
