<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useModalFocus } from '@/composables/useModalFocus'

defineOptions({ inheritAttrs: false })
const props = withDefaults(defineProps<{ visible: boolean; header: string; busy?: boolean }>(), { busy: false })
const emit = defineEmits<{ close: []; 'update:visible': [value: boolean] }>()
const mounted = ref(false), content = ref<HTMLElement | null>(null)
// TDesign's initial-visible path skips its visibility watcher and keyboard setup.
const shown = computed(() => mounted.value && props.visible)
function close() { if (!props.busy) { emit('close'); emit('update:visible', false) } }
const { focusInside } = useModalFocus(shown, () => content.value?.closest<HTMLElement>('[role="dialog"]') || null, () => props.busy, close)
onMounted(() => { mounted.value = true })
watch(() => props.busy, () => nextTick(focusInside))
</script>

<template>
  <t-dialog v-bind="$attrs" :visible="shown" :header="header" attach="body" role="dialog" aria-modal="true" :aria-label="header" tabindex="-1" :close-btn="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="false" @opened="focusInside" @close="close">
    <div ref="content"><slot /></div>
  </t-dialog>
</template>
