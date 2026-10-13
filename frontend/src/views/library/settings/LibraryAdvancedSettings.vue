<template>
  <div class="library-advanced-settings" :class="{ 'library-advanced-settings--embedded': embedded }">
    <div v-if="!embedded" class="section-header">
      <h2>{{ $t('knowledgeEditor.advanced.title') }}</h2>
      <p class="section-description">{{ $t('knowledgeEditor.advanced.description') }}</p>
    </div>

    <div class="settings-group">







      <div class="setting-row setting-row-vertical">
        <div class="setting-info">
          <label>{{ $t('knowledgeEditor.advanced.tableMetadataInstructions.label') }}</label>
          <p class="desc">{{ $t('knowledgeEditor.advanced.tableMetadataInstructions.description') }}</p>
        </div>
        <div class="setting-control">
          <SafeTextarea
            :model-value="tableMetadataInstructions"
            :placeholder="$t('knowledgeEditor.advanced.tableMetadataInstructions.placeholder')"
            :maxlength="4000"
            :autosize="{ minRows: 3, maxRows: 8 }"
            @change="(value: string) => emit('update:tableMetadataInstructions', value)"
          />
        </div>
      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import SafeTextarea from '@/components/SafeTextarea.vue'

interface Props {
  embedded?: boolean
  tableMetadataInstructions?: string
}

const props = withDefaults(defineProps<Props>(), {
  embedded: false,
})

const emit = defineEmits<{
  'update:tableMetadataInstructions': [value: string]
}>()

</script>

<style lang="less" scoped>
.library-advanced-settings {
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

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 0;
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

  .hint {
    font-size: 12px;
    color: var(--td-text-color-placeholder);
    margin: 6px 0 0 0;
    line-height: 1.5;
  }
}

.setting-control {
  flex: 0 0 55%;
  max-width: 55%;
  display: flex;
  justify-content: flex-end;
  align-items: center;
}

.setting-row-vertical {
  flex-direction: column;
  gap: 12px;

  .setting-info,
  .setting-control {
    flex: none;
    width: 100%;
    max-width: none;
    padding-right: 0;
  }

  .setting-control {
    display: block;
  }
}

.subsection {
  padding: 16px 20px;
  margin: 12px 0 0 0;
  background: var(--td-bg-color-container);
  border-radius: 8px;
  border-left: 3px solid var(--td-brand-color);
  position: relative;
}

.required {
  color: var(--td-error-color);
  margin-left: 2px;
  font-weight: 500;
}

.library-advanced-settings--embedded {
  .setting-row {
    padding: 12px 0;
  }

  .setting-row:has(.t-switch) {
    flex-direction: row;
    align-items: center;
    gap: 16px;

    .setting-info {
      flex: 1;
      min-width: 0;
      max-width: none;
      padding-right: 0;
    }

    .setting-control {
      flex: none;
      align-self: center;
    }
  }

  .subsection .setting-row {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;

    .setting-info {
      flex: none;
      max-width: none;
      padding-right: 0;
    }

    .setting-control {
      align-self: flex-start;
    }
  }

  .subsection {
    margin-top: 0;
    padding: 0;
    border: none;
    background: none;
  }
}

</style>
