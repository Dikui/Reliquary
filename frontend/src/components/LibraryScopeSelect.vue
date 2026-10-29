<script setup lang="ts">
import { computed, onMounted, ref, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
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
    <select v-if="options.length" class="rq-control" :multiple="!single" :value="single ? props.modelValue[0] : undefined" :disabled="disabled || loading" :aria-label="t('ikb18.scope')" @change="emit('update:modelValue', Array.from(($event.target as HTMLSelectElement).selectedOptions, option => option.value))"><option v-for="option in options" :key="option.value" :value="option.value" :selected="!single && props.modelValue.includes(option.value)">{{ option.label }}</option></select>
    <span v-else-if="loading" role="status">{{ t('common.loading') }}</span>
    <p v-else-if="!error" role="status">{{ t('libraryNavigation.empty') }}</p>
    <p v-if="error" role="alert">{{ error }} <Button @click="load">{{ t('common.retry') }}</Button></p>
  </div>
</template>
<style scoped>
.library-scope { min-width: 0; width: 100%; max-width: 440px; }
.library-scope p { font-size: 12px; }
.library-scope [role="alert"] { color: var(--danger); }

</style>
