<template>
  <section class="not-found">
    <t-icon name="error-circle" size="48px" aria-hidden="true" />
    <h1>{{ t('navigation.pageNotFound') }}</h1>
    <p>{{ t('navigation.pageNotFoundDescription') }}</p>
    <t-button theme="primary" @click="router.push('/platform/knowledge-bases')">
      {{ t('navigation.returnToKnowledgeBases') }}
    </t-button>
  </section>
</template>

<script setup lang="ts">
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
  color: var(--td-text-color-primary);
  background: var(--td-bg-color-container);
  overflow-wrap: anywhere;
}
h1, p { margin: 0; }
p { color: var(--td-text-color-secondary); line-height: 1.6; }
</style>
