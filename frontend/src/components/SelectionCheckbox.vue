<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{ checked: boolean; disabled?: boolean }>()
const emit = defineEmits<{ change: [checked: boolean, ctx: { e: Event }] }>()
const gesture = ref<MouseEvent | null>(null)
let suppressClick = false

function onMouseDown(e: MouseEvent) {
  gesture.value = null
  suppressClick = false
  if (props.disabled || e.button !== 0 || !(e.shiftKey || e.ctrlKey || e.metaKey)) return
  // Modifier selection must not depend on the browser forwarding a label click.
  e.preventDefault()
  e.stopPropagation()
  gesture.value = e
}

function onMouseUp(e: MouseEvent) {
  const start = gesture.value
  gesture.value = null
  if (!start || e.button !== 0) return
  e.preventDefault()
  e.stopPropagation()
  suppressClick = true
  if (!props.disabled) emit('change', !props.checked, { e: start })
}

function onClick(e: MouseEvent) {
  if (!suppressClick) return
  suppressClick = false
  e.preventDefault()
  e.stopPropagation()
}

function onContextMenu(e: MouseEvent) {
  if (e.ctrlKey) e.preventDefault()
}
</script>

<template>
  <t-checkbox :checked="checked" :disabled="disabled"
    @change="(value: boolean, ctx: { e: Event }) => emit('change', value, ctx)"
    @mousedown.capture="onMouseDown" @mouseup.capture="onMouseUp"
    @click.capture="onClick" @contextmenu.capture="onContextMenu">
    <slot />
  </t-checkbox>
</template>
