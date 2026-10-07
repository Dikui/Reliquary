<template>
  <div class="kb-vector-store-settings">
    <div class="section-header">
      <h2>{{ $t('kbSettings.vectorStore.title') }}</h2>
      <p class="section-description">{{ $t('kbSettings.vectorStore.description') }}</p>
    </div>
    <div class="settings-group">
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('kbSettings.vectorStore.engineLabel') }}</label>
          <p class="desc">{{ $t('kbSettings.vectorStore.engineDesc') }}</p>
        </div>
        <div class="setting-control">
          <VectorStoreBadge source="env" name="PostgreSQL" engine-type="postgres" :status="props.boundStatus" />
          <p v-if="props.boundStatus === 'unavailable'" class="option-hint change-warning">
            {{ $t('kbSettings.vectorStore.unavailableHint') }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { VectorStoreStatus } from '@/api/knowledge-base'
import VectorStoreBadge from '@/components/VectorStoreBadge.vue'

const props = defineProps<{ boundStatus?: VectorStoreStatus }>()
</script>

<style lang="less" scoped>
.kb-vector-store-settings {
  width: 100%;
}

.section-header {
  margin-bottom: 20px;

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 6px 0;
  }

  .section-description {
    font-size: 14px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.loading-inline {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px 0;
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.settings-group {
  display: flex;
  flex-direction: column;
}

.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 16px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child {
    border-bottom: none;
  }
}

.setting-info {
  flex: 0 0 40%;
  max-width: 40%;
  padding-right: 24px;

  label {
    font-size: 15px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    display: block;
    margin-bottom: 4px;
  }

  .desc {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.setting-control {
  flex: 0 0 55%;
  max-width: 55%;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
}

.select-option {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.option-hint {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  margin: 0;
  line-height: 1.4;

  &.change-warning {
    color: var(--td-warning-color);
  }
}

.go-settings {
  font-size: 13px;
  color: var(--td-brand-color, #0052d9);
  margin-top: 8px;
  text-decoration: none;

  &:hover {
    text-decoration: underline;
  }
}
</style>
