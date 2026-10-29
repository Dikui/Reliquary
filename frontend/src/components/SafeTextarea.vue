<template>
  <TDesignTextarea ref="textareaRef" v-bind="$attrs" :class="{ 'safe-textarea-autosize': autosize }"
    :default-value="initialValue"
    :placeholder="placeholder" :autosize="false" @input="scheduleResize" @change="handleChange">
    <template v-for="(_, name) in $slots" #[name]="slotProps">
      <slot :name="name" v-bind="slotProps || {}" />
    </template>
  </TDesignTextarea>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, onUpdated, ref, watch } from 'vue'
import { Textarea as TDesignTextarea } from 'tdesign-vue-next'
import { calculateTextareaHeight, type TextareaAutosize } from '@/utils/textareaAutosize'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{
  modelValue?: unknown
  value?: unknown
  placeholder?: string
  autosize?: TextareaAutosize
}>(), { autosize: false })
const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:value': [value: string]
  change: [value: string, context?: { e?: InputEvent }]
}>()

const textareaRef = ref<{ $el: HTMLElement; focus: () => void; blur: () => void } | null>(null)
const initialValue = String(props.modelValue ?? props.value ?? '')
let syncing = false
let mounted = false
let pending = false
let observer: ResizeObserver | undefined

const textareaElement = () => textareaRef.value?.$el?.querySelector('textarea') as HTMLTextAreaElement | null

function scheduleResize() {
  if (!mounted || !props.autosize || pending) return
  pending = true
  void nextTick(() => {
    pending = false
    const textarea = textareaElement()
    // TDesign queues this work without checking its ref after unmount. Only
    // measure a live visible textarea, including after a v-show pane opens.
    if (!mounted || !props.autosize || !(textarea instanceof HTMLTextAreaElement)
      || !textarea.isConnected || !textarea.getClientRects().length) return
    Object.assign(textarea.style, calculateTextareaHeight(textarea, props.autosize), { resize: 'none' })
  })
}

function handleChange(value: string | number, context?: { e?: InputEvent }) {
  scheduleResize()
  if (syncing) return
  const text = String(value)
  emit('update:modelValue', text)
  emit('update:value', text)
  emit('change', text, context)
}

// TDesign's controlled setInputValue writes to a readonly prop ref after input.
// Keep its writable internal value and synchronize external edits through input.
function syncExternalValue() {
  const value = props.modelValue ?? props.value
  const textarea = textareaElement()
  if (value === undefined || !textarea || textarea.value === String(value)) return
  syncing = true
  try {
    textarea.value = String(value)
    textarea.dispatchEvent(new Event('input'))
  } finally { syncing = false }
  scheduleResize()
}
watch(() => [props.modelValue, props.value], syncExternalValue, { flush: 'post' })

watch(() => [props.modelValue, props.value, props.placeholder, props.autosize], scheduleResize, { deep: true, flush: 'post' })
onUpdated(scheduleResize)
onMounted(() => {
  mounted = true
  const textarea = textareaElement()
  if (textarea && typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(scheduleResize)
    observer.observe(textarea)
  }
  window.addEventListener('resize', scheduleResize)
  scheduleResize()
})
onBeforeUnmount(() => {
  mounted = false
  observer?.disconnect()
  window.removeEventListener('resize', scheduleResize)
})

defineExpose({
  focus: () => textareaRef.value?.focus(),
  blur: () => textareaRef.value?.blur(),
})
</script>

<style scoped>
.safe-textarea-autosize :deep(.t-textarea__inner) {
  resize: none;
}
</style>
