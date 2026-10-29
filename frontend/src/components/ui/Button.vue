<script setup lang="ts">
withDefaults(defineProps<{
  variant?: 'primary' | 'default' | 'ghost' | 'danger'
  type?: 'button' | 'submit' | 'reset'
  disabled?: boolean
  loading?: boolean
}>(), { variant: 'default', type: 'button' })
</script>

<template>
  <button :type="type" class="rq-button" :class="`rq-button--${variant}`" :disabled="disabled || loading" :aria-busy="loading || undefined">
    <span class="rq-button__label" :class="{ 'rq-button__label--busy': loading }"><slot /></span>
    <span v-if="loading" class="rq-spinner" aria-hidden="true" />
  </button>
</template>
