<script setup lang="ts">
import { TooltipProvider, TooltipRoot, TooltipTrigger, TooltipPortal, TooltipContent } from 'reka-ui'
defineProps<{ content: string }>()
</script>
<template>
  <TooltipProvider :delay-duration="300">
    <TooltipRoot>
      <TooltipTrigger as-child><slot /></TooltipTrigger>
      <TooltipPortal><TooltipContent class="rq-tooltip" :side-offset="6">{{ content }}</TooltipContent></TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>
