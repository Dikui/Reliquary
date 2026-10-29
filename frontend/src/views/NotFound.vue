<template>
  <section class="not-found">
    <h1>{{ t('navigation.pageNotFound') }}</h1>
    <p>{{ t('navigation.pageNotFoundDescription') }}</p>
    <Button variant="primary" @click="router.push('/platform/entries')">
      {{ t('navigation.returnToEntries') }}
    </Button>
  </section>
</template>

<script setup lang="ts">
import { Button } from '@/components/ui'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

const { t } = useI18n()
const router = useRouter()
</script>

<style scoped lang="less">
.not-found {
  flex: 1;
  min-width: 0;
  padding: 32px 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  text-align: center;
  color: var(--text);
  background: var(--surface);
  overflow-wrap: anywhere;
}
h1, p { margin: 0; }
p { color: var(--muted); line-height: 1.6; }
</style>
