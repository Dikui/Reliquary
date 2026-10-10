import assert from 'node:assert/strict'
import test from 'node:test'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

const fullSectionWidths: Record<number, number> = {
  1440: 804,
  1280: 804,
  1024: 728,
  900: 636,
  768: 504,
}

test('system settings respond to the available settings content width', async () => {
  const rules = await compiledSFCStyleRules(new URL('./SystemSettings.vue', import.meta.url))
  for (const viewport of [900, 768]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, {
      'settings-content': fullSectionWidths[viewport],
    })
    assert.equal(styles('.runtime-table-header').display, 'none')
    assert.equal(styles('.settings-group--runtime .setting-row').display, 'flex')
    assert.equal(styles('.settings-group--runtime .setting-row')['flex-direction'], 'column')
    assert.equal(styles('.setting-info')['min-width'], '0')
    assert.equal(styles('.setting-info')['max-width'], '100%')
    assert.equal(styles('.setting-control')['min-width'], '0')
    assert.equal(styles('.setting-control')['max-width'], '100%')
    assert.equal(styles('.setting-control-row')['flex-wrap'], 'wrap')
    assert.equal(styles('.setting-input')['min-width'], '0')
    assert.equal(styles('.setting-input')['max-width'], '100%')
  }
  for (const viewport of [1440, 1280, 1024]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, {
      'settings-content': fullSectionWidths[viewport],
    })
    assert.equal(styles('.runtime-table-header').display, 'grid')
    assert.equal(styles('.settings-group--runtime .setting-row')['grid-template-columns'], 'minmax(0, 1fr) 280px')
    assert.equal(styles('.setting-control')['min-width'], '280px')
    assert.equal(styles('.setting-input--wide').width, '320px')
  }
})

test('runtime cards reflow and tables scroll within the narrow settings content', async () => {
  const rules = await compiledSFCStyleRules(new URL('./RuntimeQueues.vue', import.meta.url))
  for (const viewport of [900, 768]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, {
      'settings-content': fullSectionWidths[viewport],
    })
    assert.equal(styles('.rq-header')['flex-direction'], 'column')
    assert.equal(styles('.rq-actions')['flex-wrap'], 'wrap')
    assert.equal(styles('.rq-overview')['flex-direction'], 'column')
    assert.equal(styles('.rq-overview-metrics')['grid-template-columns'], 'repeat(2, minmax(0, 1fr))')
    assert.equal(styles('.rq-pool-grid')['grid-template-columns'], 'repeat(2, minmax(0, 1fr))')
    assert.equal(styles('.rq-metric-label')['white-space'], 'normal')
    assert.equal(styles('.data-table-shell.rq-table-shell')['overflow-x'], 'auto')
    assert.equal(styles('.data-table-shell.rq-table-shell')['max-width'], '100%')
    assert.equal(styles('.rq-table-shell table')['min-width'], '794px')
    assert.equal(styles('.rq-models .rq-table-shell table')['min-width'], '698px')
  }
  for (const viewport of [1440, 1280, 1024]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, {
      'settings-content': fullSectionWidths[viewport],
    })
    assert.equal(styles('.rq-pool-grid')['grid-template-columns'], 'repeat(4, minmax(0, 1fr))')
    assert.equal(styles('.rq-header')['flex-direction'], undefined)
    assert.equal(styles('.rq-pool-name')['min-width'], '0')
    assert.equal(styles('.rq-pool-name')['overflow-wrap'], 'anywhere')
  }
})

test('API settings keep controls and paginated tables inside the narrow content', async () => {
  const rules = await compiledSFCStyleRules(new URL('../integrations/ApiIntegrationSettings.vue', import.meta.url))
  for (const viewport of [900, 768]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, {
      'settings-content': fullSectionWidths[viewport],
    })
    assert.equal(styles('.setting-row')['flex-direction'], 'column')
    assert.equal(styles('.setting-info')['min-width'], '0')
    assert.equal(styles('.setting-control')['min-width'], '0')
    assert.equal(styles('.setting-control')['max-width'], '100%')
    assert.equal(styles('.api-table-shell .t-table__content')['overflow-x'], 'auto')
    assert.equal(styles('.api-table-shell table')['min-width'], '640px')
    assert.equal(styles('.api-table-shell .t-pagination')['flex-wrap'], 'wrap')
  }
  for (const viewport of [1440, 1280, 1024]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, {
      'settings-content': fullSectionWidths[viewport],
    })
    assert.equal(styles('.setting-row')['flex-direction'], undefined)
    assert.equal(styles('.api-table-shell table')['min-width'], undefined)
  }
})

test('teleported runtime task drawer handles narrow content without the settings container', async () => {
  const rules = await compiledSFCStyleRules(new URL('./RuntimeQueues.vue', import.meta.url))
  for (const viewport of [1440, 1280, 1024, 900, 768]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport)
    assert.equal(styles('.rq-task-state-filter')['overflow-x'], 'auto')
    assert.equal(styles('.rq-task-state-option')['min-width'], '104px')
    assert.equal(styles('.rq-failed-section-head')['flex-wrap'], 'wrap')
    assert.equal(styles('.rq-failed-section-actions')['flex-wrap'], 'wrap')
    assert.equal(styles('.rq-failed-section-actions')['max-width'], '100%')
  }
})
