import assert from 'node:assert/strict'
import test from 'node:test'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

for (const inner of [804, 728, 636, 504, 358]) {
  test(`settings form controls fit ${inner}px content without forcing horizontal page scroll`, async () => {
    const rules = await compiledSFCStyleRules(new URL('../../styles/settings-form.css', import.meta.url))
    const style = (selector: string) => styleDeclarationsAtWidth(rules, selector, 1440, { 'settings-content': inner })
    assert.equal(style('.settings-form .setting-row')['grid-template-columns'], inner <= 720 ? 'minmax(0, 1fr)' : '224px minmax(0, 1fr)')
    assert.equal(style('.settings-form .setting-control')['min-width'], '0')
  })
}

test('runtime metrics wrap and wide tables scroll within their own shared container', async () => {
  const rules = await compiledSFCStyleRules(new URL('./RuntimeQueues.vue', import.meta.url))
  const style = (selector: string) => styleDeclarationsAtWidth(rules, selector, 390)
  assert.equal(style('.rq-overview-metrics')['grid-template-columns'], 'repeat(auto-fit, minmax(140px, 1fr))')
  assert.equal(style('.rq-pool-grid')['grid-template-columns'], 'repeat(auto-fit, minmax(140px, 1fr))')
  assert.equal(style('.rq-table-shell')['min-width'], '0')
  assert.equal(style('.rq-table-shell .rq-table')['min-width'], '820px')
  assert.equal(style('.rq-models .rq-table')['min-width'], '640px')
})

test('API key tables and task drawer controls retain narrow screen access', async () => {
  const api = await compiledSFCStyleRules(new URL('../integrations/ApiIntegrationSettings.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(api, '.api-settings .rq-table', 390)['min-width'], '640px')
  const queues = await compiledSFCStyleRules(new URL('./RuntimeQueues.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(queues, '.rq-task-state-filter', 390)['flex-wrap'], 'wrap')
  assert.equal(styleDeclarationsAtWidth(queues, '.rq-failed-section-actions', 390)['flex-wrap'], 'wrap')
})
