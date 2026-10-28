<script setup lang="ts">
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuPortal, DropdownMenuContent, DropdownMenuItem } from 'reka-ui'
defineProps<{ items: { value: string; label: string; disabled?: boolean; danger?: boolean }[] }>()
const emit = defineEmits<{ select: [value: string] }>()
</script>
<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger as-child><slot /></DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent class="rq-menu" :side-offset="4" align="start">
        <DropdownMenuItem v-for="item in items" :key="item.value" class="rq-menu__item" :class="{ 'rq-menu__item--danger': item.danger }" :disabled="item.disabled" @select="emit('select', item.value)">{{ item.label }}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>
