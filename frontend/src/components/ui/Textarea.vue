<script setup lang="ts">
import { ref } from 'vue'
import { useFieldAttrs } from './field'
defineOptions({ inheritAttrs: false })
withDefaults(defineProps<{ modelValue?: string }>(), { modelValue: '' })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const attrs = useFieldAttrs()
const input = ref<HTMLTextAreaElement>()
let composing = false
function startComposition() { composing = true }
function endComposition(event: Event) { composing = false; update(event) }
function update(event: Event) {
  if (!composing) emit('update:modelValue', (event.target as HTMLTextAreaElement).value)
}
defineExpose({ focus: () => input.value?.focus(), select: () => input.value?.select() })
</script>

<template>
  <textarea ref="input" v-bind="attrs" class="rq-control rq-textarea" :value="modelValue" @input="update" @compositionstart="startComposition" @compositionend="endComposition" />
</template>
