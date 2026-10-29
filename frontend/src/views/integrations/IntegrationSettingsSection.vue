<template>
  <div class="integrations-settings">
    <div class="integrations-settings__body" >
      <div v-if="tab === 'api'" class="section">
        <div class="section-header">
          <h2>{{ $t('integrations.api.title') }}</h2>
          <p class="section-description">{{ $t('integrations.api.subtitle') }}</p>
        </div>
        <ApiIntegrationSettings />
      </div>



    </div>
  </div>
</template>

<script setup lang="ts">
import ApiIntegrationSettings from '@/views/integrations/ApiIntegrationSettings.vue'
import type { IntegrationTab } from '@/config/integrations'

const props = defineProps<{
  tab: IntegrationTab
}>()

</script>

<style scoped lang="less">
.integrations-settings {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.integrations-settings__body {
  min-width: 0;
}

.integrations-settings__body--landing {
  max-width: 760px;
}

.section-header {
  margin-bottom: 18px;

  h2 {
    margin: 0 0 6px;
    color: var(--td-text-color-primary);
    font-size: 18px;
    font-weight: 600;
    line-height: 1.35;
  }
}

.section-description {
  margin: 0;
  color: var(--td-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.doc-link {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  margin-left: 6px;
  color: var(--td-brand-color);
  text-decoration: none;

  &:hover {
    text-decoration: underline;
  }
}

.link-icon {
  font-size: 13px;
}
</style>
