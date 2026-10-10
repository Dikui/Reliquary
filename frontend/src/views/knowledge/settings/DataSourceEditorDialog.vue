<script setup lang="ts">
import SafeTextarea from '@/components/SafeTextarea.vue'
import { computed, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import { createDataSource, updateDataSource, triggerSync, validateCredentials, putDataSourceCredentials, deleteDataSourceCredentials, type DataSource } from '@/api/datasource'

const props = defineProps<{ kbId: string; dataSource: DataSource | null }>()
const visible = defineModel<boolean>('visible', { default: false })
const emit = defineEmits<{ saved: [] }>()
const { t } = useI18n()
const submitting = ref(false)
const testing = ref(false)
const error = ref('')
const testResult = ref('')
const configured = ref(false)
const replaceCredentials = ref(false)
const form = ref({ name: '', feedUrls: '', publisher: '', articlePathPrefix: '', authHeaders: '', sync_schedule: '0 0 2 * * *', sync_mode: 'incremental' as 'incremental' | 'full', sync_deletions: true })
const scheduleOptions = computed(() => [
  { label: t('datasource.schedule30min'), value: '0 */30 * * * *' },
  { label: t('datasource.schedule1h'), value: '0 0 * * * *' },
  { label: t('datasource.schedule6h'), value: '0 0 */6 * * *' },
  { label: t('datasource.schedule12h'), value: '0 0 */12 * * *' },
  { label: t('datasource.schedule24h'), value: '0 0 2 * * *' },
])
const rssPresets = [
  ['OpenAI', 'https://openai.com/news/rss.xml', 'OpenAI'],
  ['Anthropic', 'https://www.anthropic.com/news', 'Anthropic', '/news/'],
  ['claude.dev', 'https://claude.dev/rss.xml', 'Anthropic'],
  ['DeepSeek', 'https://api-docs.deepseek.com/zh-cn/updates/', 'DeepSeek', '/zh-cn/updates/'],
  ['Qwen', 'https://qwen.ai/blog', 'Qwen', '/blog'],
  ['Kimi', 'https://www.kimi.ai/blog/', 'Kimi', '/blog/'],
  ['Z.ai', 'https://www.zhipuai.cn/zh/research', 'Z.ai', '/zh/research'],
  ['MiniMax', 'https://www.minimax.io/news', 'MiniMax', '/news'],
  ['ByteDance Seed', 'https://seed.bytedance.com/zh/', 'ByteDance Seed', '/zh/blog/'],
  ['Simon Willison', 'https://simonwillison.net/atom/everything/', 'Simon Willison'],
  ['Google DeepMind', 'https://deepmind.google/blog/rss.xml', 'Google DeepMind'],
  ['Meta AI', 'https://ai.meta.com/blog/', 'Meta AI', '/blog/'],
  ['Mistral AI', 'https://mistral.ai/news/', 'Mistral AI', '/news/'],
  ['NVIDIA', 'https://blogs.nvidia.com/feed/', 'NVIDIA'],
  ['Hugging Face', 'https://huggingface.co/blog/feed.xml', 'Hugging Face'],
  ['TechCrunch AI', 'https://techcrunch.com/category/artificial-intelligence/feed/', 'TechCrunch'],
  ['Artificial Analysis', 'https://artificialanalysis.ai/articles', 'Artificial Analysis', '/articles/'],
  ['Cursor', 'https://cursor.com/blog', 'Cursor', '/blog/'],
  ['Ethan Mollick', 'https://www.oneusefulthing.org/feed', 'Ethan Mollick'],
  ['Latent Space', 'https://www.latent.space/feed', 'Latent Space'],
  ['宝玉', 'https://baoyu.io/feed.xml', '宝玉'],
] as const
function selectPreset(name: string, url: string, publisher: string, prefix = '') { Object.assign(form.value, { name, feedUrls: url, publisher, articlePathPrefix: prefix }); testResult.value = '' }
watch(visible, (open) => {
  if (!open) { form.value.authHeaders = ''; return }
  const ds = props.dataSource
  const settings = ds?.config?.settings || {}
  form.value = { name: ds?.name || '', feedUrls: settings.feed_urls || ds?.config?.resource_ids?.join('\n') || '', publisher: settings.publisher || '', articlePathPrefix: settings.article_path_prefix || '', authHeaders: '', sync_schedule: ds?.sync_schedule || '0 0 2 * * *', sync_mode: ds?.sync_mode || 'incremental', sync_deletions: ds?.sync_deletions ?? true }
  configured.value = ds?.credentials?.credentials?.configured === true
  replaceCredentials.value = false
  error.value = ''
  testResult.value = ''
})
async function testConnection() {
  if (!form.value.feedUrls.trim() || testing.value) return
  testing.value = true
  error.value = ''
  try { await validateCredentials('rss', { feed_urls: form.value.feedUrls, auth_headers: form.value.authHeaders, publisher: form.value.publisher, article_path_prefix: form.value.articlePathPrefix }); testResult.value = t('datasource.connected') }
  catch (e: any) { error.value = e.message || t('datasource.connectionFailed') }
  finally { testing.value = false }
}
async function removeCredentials() {
  if (!props.dataSource) return
  try { await deleteDataSourceCredentials(props.dataSource.id); configured.value = false; replaceCredentials.value = false; form.value.authHeaders = ''; MessagePlugin.success(t('credential.removedToast')) }
  catch (e: any) { error.value = e.message || t('credential.removeFailed') }
}
async function save() {
  if (submitting.value) return
  if (!form.value.name.trim() || !form.value.feedUrls.trim()) { error.value = t('datasource.requiredFields'); return }
  submitting.value = true
  error.value = ''
  try {
    const credentials = form.value.authHeaders.trim() ? { auth_headers: form.value.authHeaders } : {}
    const settings = { feed_urls: form.value.feedUrls.trim(), publisher: form.value.publisher, article_path_prefix: form.value.articlePathPrefix }
    const payload: Partial<DataSource> = { name: form.value.name.trim(), type: 'rss', knowledge_base_id: props.kbId, config: { credentials: props.dataSource ? {} : credentials, settings, resource_ids: [] }, sync_schedule: form.value.sync_schedule, sync_mode: form.value.sync_mode, sync_deletions: form.value.sync_deletions, status: 'active' }
    if (props.dataSource) {
      if (replaceCredentials.value) await putDataSourceCredentials(props.dataSource.id, credentials)
      await updateDataSource(props.dataSource.id, payload)
      MessagePlugin.warning(t('datasource.updateSuccessSyncHint'))
    } else {
      const response: any = await createDataSource(payload)
      const id = (response.data || response).id
      try { await triggerSync(id); MessagePlugin.success(t('datasource.createAndSyncSuccess')) }
      catch (e: any) { MessagePlugin.warning(e.message || t('datasource.createButSyncFailed')) }
    }
    emit('saved')
    visible.value = false
  } catch (e: any) { error.value = e.message || t('datasource.saveFailed') }
  finally { submitting.value = false }
}
</script>

<template>
  <SettingDrawer v-model:visible="visible" :title="t(dataSource ? 'datasource.edit' : 'datasource.add')" :confirm-text="t('common.save')" :confirm-loading="submitting" storage-key="setting-drawer:width:datasource-editor" width="640px" @confirm="save">
    <template #footer-left><t-button variant="outline" :loading="testing" :disabled="submitting || (configured && !replaceCredentials)" @click="testConnection">{{ t('datasource.testConnection') }}</t-button></template>
    <t-form :data="form" layout="vertical" label-align="top" @submit.prevent>
      <t-form-item :label="t('datasource.nameLabel')" name="name"><t-input v-model="form.name" :disabled="submitting" /></t-form-item>
      <div class="rss-presets"><t-button v-for="[name, url, publisher, prefix] in rssPresets" :key="url" variant="outline" size="small" :disabled="submitting" @click="selectPreset(name, url, publisher, prefix)">{{ name }}</t-button></div>
      <t-form-item :label="t('datasource.field.feedUrls')" name="feedUrls"><SafeTextarea v-model="form.feedUrls" :autosize="{ minRows: 3, maxRows: 8 }" :disabled="submitting" /><p>{{ t('datasource.field.feedUrlsHint') }}</p></t-form-item>
      <t-form-item :label="t('datasource.field.feedPublisher')" name="publisher"><t-input v-model="form.publisher" :disabled="submitting" /></t-form-item>
      <t-form-item :label="t('datasource.field.articlePathPrefix')" name="articlePathPrefix"><t-input v-model="form.articlePathPrefix" :disabled="submitting" /><p>{{ t('datasource.field.articlePathPrefixHint') }}</p></t-form-item>
      <div v-if="configured && !replaceCredentials" class="credential-actions"><span>{{ t('credential.configured') }}</span><t-button variant="text" @click="replaceCredentials = true">{{ t('credential.update') }}</t-button><t-popconfirm :content="t('credential.confirmRemovePrompt')" @confirm="removeCredentials"><t-button variant="text" theme="danger">{{ t('credential.remove') }}</t-button></t-popconfirm></div>
      <t-form-item v-else :label="t('datasource.field.authHeaders')" name="authHeaders"><t-input v-model="form.authHeaders" type="password" :disabled="submitting" :placeholder="t('datasource.field.authHeadersHint')" /></t-form-item>
      <t-form-item :label="t('datasource.syncScheduleLabel')" name="sync_schedule"><t-select v-model="form.sync_schedule" :options="scheduleOptions" :disabled="submitting" /></t-form-item>
      <t-form-item :label="t('datasource.syncModeLabel')" name="sync_mode"><t-radio-group v-model="form.sync_mode" :disabled="submitting"><t-radio value="incremental">{{ t('datasource.syncMode.incremental') }}</t-radio><t-radio value="full">{{ t('datasource.syncMode.full') }}</t-radio></t-radio-group></t-form-item>

      <t-form-item :label="t('datasource.syncDeletions')" name="sync_deletions"><t-switch v-model="form.sync_deletions" :disabled="submitting" /></t-form-item>
      <t-alert v-if="error" theme="error" :message="error" /><t-alert v-else-if="testResult" theme="success" :message="testResult" />
    </t-form>
  </SettingDrawer>
</template>

<style scoped lang="less">
.rss-presets, .credential-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: 16px; }
p { color: var(--td-text-color-secondary); font-size: 12px; margin: 6px 0 0; }
</style>
