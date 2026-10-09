<template>
  <span class="resource-origin-badge" :class="variantClass" :title="tooltipText">
    <t-icon :name="iconName" size="12px" class="badge-icon" />
    <span class="badge-text">{{ displayText }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Icon as TIcon } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'

/** Displays the creator of a space resource. */
const props = withDefaults(
  defineProps<{
    variant: 'mine' | 'tenant' | 'creator'
    /** Optional creator display name, surfaces in tooltip for `tenant` variant. */
    creatorName?: string
  }>(),
  { creatorName: '' }
)

const { t } = useI18n()
const authStore = useAuthStore()

const iconName = computed(() => {
  switch (props.variant) {
    case 'mine':
      return 'user'
    case 'tenant':
      return 'usergroup'
    case 'creator':
      return 'user'
    default:
      return 'usergroup'
  }
})

const variantClass = computed(() => `origin-${props.variant}`)

const displayText = computed(() => {
  switch (props.variant) {
    case 'mine':
      return t('resourceOrigin.mine')
    case 'tenant':
      // Prefer the tenant name when known so the badge says where the
      // resource lives, not a vague "tenant" label. Falls back to i18n.
      return authStore.currentTenantName || t('resourceOrigin.tenant')
    case 'creator':
      // Section header already provides the「本空间」context, so we just
      // show who created it. Fall back to a generic label when the user
      // can't be resolved (creator_name 缺失，例如已删除账号 / 老数据)。
      return props.creatorName || t('resourceOrigin.tenant')
    default:
      return ''
  }
})

const tooltipText = computed(() => {
  switch (props.variant) {
    case 'mine':
      return t('resourceOrigin.mineTooltip')
    case 'tenant':
      if (props.creatorName) {
        return t('resourceOrigin.tenantTooltipWithCreator', { creator: props.creatorName })
      }
      return t('resourceOrigin.tenantTooltip')
    case 'creator':
      // 卡片标签只露名字；tooltip 把完整含义补回来。
      if (props.creatorName) {
        return t('resourceOrigin.tenantTooltipWithCreator', { creator: props.creatorName })
      }
      return t('resourceOrigin.tenantTooltip')
    default:
      return ''
  }
})
</script>

<style scoped lang="less">
.resource-origin-badge {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 6px;
  border-radius: 8px;
  font-size: 11px;
  line-height: 1.4;
  font-weight: 500;
  max-width: 140px;

  .badge-icon {
    flex-shrink: 0;
  }

  .badge-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &.origin-mine {
    color: var(--td-brand-color);
    background: var(--td-success-color-light);
  }

  &.origin-tenant {
    color: var(--td-text-color-secondary);
    background: var(--td-bg-color-secondarycontainer);
  }

  &.origin-creator {
    color: var(--td-text-color-secondary);
    background: var(--td-bg-color-secondarycontainer);
  }




}
</style>
