import assert from 'node:assert/strict'
import test from 'node:test'
import { nextTick, reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { connectSettingsNavigation } from './settingsNavigation'

const tick = async () => { await new Promise(resolve => setImmediate(resolve)); await nextTick() }
test('legacy settings requests navigate to the page, preserve subsections and obey route guards', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/platform/entries', component: {} }, { path: '/platform/settings', component: {} },
  ] })
  await router.push('/platform/entries')
  const ui = reactive({ showSettingsModal: false, settingsInitialSection: null as string | null,
    settingsInitialSubSection: null as string | null,
    closeSettings() { this.showSettingsModal = false; this.settingsInitialSection = null; this.settingsInitialSubSection = null },
  })
  const target = new EventTarget()
  const disconnect = connectSettingsNavigation(router, ui, target)
  ui.settingsInitialSection = 'knowledge'; ui.settingsInitialSubSection = 'models'; ui.showSettingsModal = true
  await tick()
  assert.equal(router.currentRoute.value.path, '/platform/settings')
  assert.deepEqual(router.currentRoute.value.query, { section: 'library', subsection: 'models' })
  assert.equal(ui.showSettingsModal, false)
  const block = router.beforeEach(() => false)
  target.dispatchEvent(new CustomEvent('settings-nav', { detail: { section: 'models', subsection: 'rerank' } }))
  await tick()
  assert.equal(router.currentRoute.value.query.section, 'library')
  block()
  target.dispatchEvent(new CustomEvent('settings-nav', { detail: { section: 'integrations' } }))
  await tick()
  assert.deepEqual(router.currentRoute.value.query, { section: 'integration-api' })
  disconnect()
  target.dispatchEvent(new CustomEvent('settings-nav', { detail: { section: 'general' } }))
  await tick()
  assert.equal(router.currentRoute.value.query.section, 'integration-api')
})
