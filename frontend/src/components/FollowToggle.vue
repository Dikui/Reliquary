<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useFollowStore } from '@/stores/follows'
import type { FollowTargetType } from '@/types/follows'

const props = withDefaults(defineProps<{ targetType: FollowTargetType; targetId: string; name: string; compact?: boolean }>(), { compact: false })
const { t } = useI18n()
const store = useFollowStore(), error = ref('')
const followed = computed(() => store.isFollowing(props.targetType, props.targetId))
const saving = computed(() => store.isPending(props.targetType, props.targetId))
const label = computed(() => t(followed.value ? 'follows.unfollow' : 'follows.follow'))
async function load() { try { await store.loadTargets(true) } catch { /* The shared error is shown beside each control. */ } }
async function toggle() {
  if (saving.value || store.targetsLoading || !store.targetsLoaded || store.targetsError) return
  const type = props.targetType, id = props.targetId, following = !followed.value
  error.value = ''
  try {
    await store.setFollowing(type, id, props.name, following)
    MessagePlugin.success(t(following ? 'follows.followed' : 'follows.unfollowed'))
  } catch { if (props.targetId === id && props.targetType === type) error.value = t('follows.saveFailed') }
}
watch(() => `${props.targetType}:${props.targetId}`, () => { error.value = ''; void load() })
onMounted(load)
</script>

<template>
  <span class="follow-control" :class="{ compact }" @click.stop>
    <t-button size="small" :variant="compact ? 'text' : 'outline'" :theme="followed ? 'primary' : 'default'" :loading="saving || store.targetsLoading" :disabled="saving || store.targetsLoading || !store.targetsLoaded || store.targetsError" :aria-pressed="followed" :aria-label="t(followed ? 'follows.unfollowNamed' : 'follows.followNamed', { name })" @click="toggle">
      <template #icon><t-icon :name="followed ? 'check' : 'add'" /></template>{{ label }}
    </t-button>
    <span v-if="error" class="follow-error" role="alert">{{ error }}</span>
    <span v-if="store.targetsError" class="follow-error" role="alert">{{ t('follows.targetsFailed') }} <t-button size="small" variant="text" @click="load">{{ t('common.retry') }}</t-button></span>
  </span>
</template>

<style scoped lang="less">
.follow-control { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 4px; min-width: 0; }
.follow-error { color: var(--td-error-color); font-size: 12px; overflow-wrap: anywhere; }
.compact :deep(.t-button) { height: 24px; font-size: 12px; padding-inline: 4px; }
</style>
