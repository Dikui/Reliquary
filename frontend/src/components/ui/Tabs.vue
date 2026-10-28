<script setup lang="ts">
import { TabsRoot, TabsList, TabsTrigger, TabsContent } from 'reka-ui'
defineProps<{ modelValue: string; label: string; items: { value: string; label: string; disabled?: boolean }[] }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
</script>
<template>
  <TabsRoot :model-value="modelValue" class="rq-tabs" @update:model-value="emit('update:modelValue', String($event))">
    <TabsList class="rq-tabs__list" :aria-label="label">
      <TabsTrigger v-for="item in items" :key="item.value" :value="item.value" :disabled="item.disabled" class="rq-tabs__trigger">{{ item.label }}</TabsTrigger>
    </TabsList>
    <TabsContent v-for="item in items" :key="item.value" :value="item.value" class="rq-tabs__panel"><slot :name="item.value" /></TabsContent>
  </TabsRoot>
</template>
