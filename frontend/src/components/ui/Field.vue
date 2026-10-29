<script setup lang="ts">
import { computed, provide, useId } from 'vue'
import { fieldKey } from './field'

const props = defineProps<{ label: string; id?: string; hint?: string; error?: string }>()
const generatedId = useId()
const id = computed(() => props.id || `rq-field-${generatedId}`)
provide(fieldKey, computed(() => ({
  id: id.value,
  invalid: !!props.error,
  describedBy: [props.hint && `${id.value}-hint`, props.error && `${id.value}-error`].filter(Boolean).join(' ') || undefined,
})))
</script>

<template>
  <div class="rq-field">
    <label :for="id" class="rq-field__label">{{ label }}</label>
    <slot :id="id" />
    <p v-if="hint" :id="`${id}-hint`" class="rq-field__hint">{{ hint }}</p>
    <p v-if="error" :id="`${id}-error`" class="rq-field__error" role="alert">{{ error }}</p>
  </div>
</template>
