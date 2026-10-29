import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import { loadUiComponent } from '../../../components/ui/uiTestLoader'
import { descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'

for (const language of ['zh-CN', 'en-US']) {
  for (const count of [0, 1, 2]) {
    for (const status of ['accepted', 'rejected']) {
      test(`${language}: ${status} history remains accessible with ${count} suggestions`, async t => {
        const component = loadComponent(new URL('./MergeSuggestions.vue', import.meta.url), {
          '@/api/entities': {}, '@/utils/search': {}, '@/components/ui/feedback': {},
          '@/components/ui': Object.fromEntries(['Button', 'Select', 'Tag', 'EmptyState', 'ErrorState'].map(name => [name, loadUiComponent(name)])),
          './MergeCandidate.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('article') }) },
        })
        const app = renderer.createApp(component, { items: Array.from({ length: count }, (_, i) => ({ id: String(i), status, reason: 'Saved rationale', new_entity_id: 'a', candidate_entity_id: 'b' })), loading: false, error: '' })
        app.use(createI18n({ legacy: false, locale: language, messages: { 'zh-CN': zh, 'en-US': en } }))
        const root = element('root'); app.mount(root); t.after(() => app.unmount())
        descendants(root).find(node => node.tag === 'select')!.props.onChange({ target: { value: status } })
        await settle()
        const html = visibleText(root)
        if (count) { assert.ok(html.includes(`1 / ${count}`)); assert.ok(html.includes('Saved rationale')) }
        else assert.ok(html.includes(language === 'zh-CN' ? '没有' : 'No'))
        assert.doesNotMatch(html, /mergeSuggestions\./)
        assert.equal(descendants(root).some(node => node.props.class?.includes('merge-decide')), false)
      })
    }
  }
}
