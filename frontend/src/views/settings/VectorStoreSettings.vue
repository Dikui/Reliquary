<template>
  <div class="vectorstore-settings">
    <div class="section-header">
      <h2>{{ t('vectorStoreSettings.title') }}</h2>
      <p class="section-description">{{ t('vectorStoreSettings.description') }}</p>
    </div>
    <t-loading :loading="loading" size="small" class="store-status-loading">
      <t-alert v-if="error" theme="error" :message="error" />
      <div v-else-if="store" class="store-card">
        <img :src="postgresLogo" alt="PostgreSQL" class="store-card__logo" />
        <div class="store-card__body">
          <h3>PostgreSQL</h3>
          <p>ParadeDB · pgvector · pg_search</p>
          <p v-if="store.connection_config?.version">{{ store.connection_config.version }}</p>
        </div>
        <t-tag :theme="store.status === 'available' ? 'success' : 'danger'" variant="light">
          {{ t(`vectorStoreSettings.${store.status === 'available' ? 'available' : 'unavailable'}`) }}
        </t-tag>
      </div>
      <t-empty v-else-if="!loading" :description="t('vectorStoreSettings.unavailable')" />
    </t-loading>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listVectorStores, type VectorStoreEntity } from '@/api/vector-store'
import postgresLogo from '@/assets/img/providers/color/vectorstore/postgres.svg?url'

const { t } = useI18n()
const loading = ref(true)
const error = ref('')
const store = ref<VectorStoreEntity | null>(null)

onMounted(async () => {
  try {
    const response = await listVectorStores()
    if (!response.success) throw new Error(t('vectorStoreSettings.loadFailed'))
    store.value = response.data[0] || null
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : t('vectorStoreSettings.loadFailed')
  } finally {
    loading.value = false
  }
})
</script>

<style lang="less" scoped>
.vectorstore-settings {
  width: 100%;
}
.section-header {
  margin-bottom: 32px;
  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 8px;
  }
}
.section-description {
  font-size: 14px;
  color: var(--td-text-color-secondary);
  margin: 0;
  line-height: 1.5;
}
.store-status-loading {
  min-height: 120px;
}
.store-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 10px;
  background: var(--td-bg-color-container);
}
.store-card__logo {
  width: 40px;
  height: 40px;
}
.store-card__body {
  flex: 1;
  min-width: 0;
  h3 {
    font-size: 16px;
    margin: 0 0 6px;
    color: var(--td-text-color-primary);
  }
  p {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    overflow-wrap: anywhere;
  }
}
</style>
