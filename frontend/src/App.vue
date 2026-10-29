<script setup lang="ts">
import ReparseConfirmHost from '@/components/ReparseConfirmHost.vue'
import ServiceAvailabilityNotice from '@/components/ServiceAvailabilityNotice.vue'
import FeedbackHost from '@/components/ui/FeedbackHost.vue'
</script>
<template>
  <div class="rq-app">
    <ServiceAvailabilityNotice />
    <RouterView />
    <ReparseConfirmHost />
    <FeedbackHost />
  </div>
</template>
