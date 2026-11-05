<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { entityMatchNames } from '@/utils/entityAutoMatch'
const props = defineProps<{ name: string; code: string; aliases: string[] }>()
const model = defineModel<Record<string, boolean>>({ required: true })
const { t } = useI18n()
function override(key: string) { return Object.hasOwn(model.value, key) ? model.value[key] : undefined }
const names = computed(() => entityMatchNames([props.name, props.code, ...props.aliases]))
</script>
<template>
  <fieldset v-if="names.length" class="name-matching">
    <legend>{{ t('managed.autoMatchTitle') }}</legend><p>{{ t('managed.autoMatchHint') }}</p>
    <label v-for="name in names" :key="name.key"><input type="checkbox" :checked="override(name.key) ?? name.enabled" @change="model = { ...model, [name.key]: ($event.target as HTMLInputElement).checked }"><span>{{ name.label }}</span><small v-if="override(name.key) === undefined">{{ t(name.enabled ? 'managed.autoMatchDefaultOn' : 'managed.autoMatchDefaultOff') }}</small></label>
  </fieldset>
</template>
<style scoped>
.name-matching { display:grid; gap:var(--space-2); padding:var(--space-3); border:1px solid var(--border); } .name-matching label { display:flex; align-items:center; gap:var(--space-2); overflow-wrap:anywhere; } .name-matching p,.name-matching small { color:var(--muted); margin:0; }
</style>
