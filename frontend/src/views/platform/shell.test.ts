import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, nextTick, reactive } from 'vue'
import { doc, mount, send, settle } from '../../components/ui/__tests__/dom'
import { loadUiComponent } from '../../components/ui/uiTestLoader'
import { loadComponent } from '../library/componentTestHarness'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

test('mobile navigation traps focus, restores the trigger and closes on route or viewport changes', async t => {
  const listeners = new Set<(event: { matches: boolean }) => void>()
  Object.assign(window, { matchMedia: () => ({ matches: true, addEventListener: (_type: string, listener: any) => listeners.add(listener), removeEventListener: (_type: string, listener: any) => listeners.delete(listener) }) })
  const route = reactive({ fullPath: '/platform/libraries', path: '/platform/libraries', query: {} as Record<string, string>, hash: '' })
  const empty = { render: () => null }
  const commands = reactive({ open: false, openPalette() { commands.open = true } })
  const menu = defineComponent({ setup: () => () => h('nav', [h('a', { href: '/platform/libraries', id: 'mobile-library' }, 'Materials'), h('button', { id: 'mobile-last' }, 'Settings')]) })
  const component = loadComponent(new URL('./index.vue', import.meta.url), {
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace() {} }), RouterView: empty, RouterLink: defineComponent({ setup: (_, { slots }) => () => h('a', slots.default?.()) }) },
    'tdesign-icons-vue-next': { MenuIcon: empty, SearchIcon: empty },
    '@/components/SideMenu.vue': { __esModule: true, default: menu },
    '@/components/ContentCheckNotice.vue': { __esModule: true, default: empty },
    '@/views/settings/Settings.vue': { __esModule: true, default: empty },
    '@/components/GlobalCommandPalette.vue': { __esModule: true, default: empty },
    '@/components/ui': { Drawer: loadUiComponent('Drawer') },
    '@/stores/commandPalette': { useCommandPaletteStore: () => commands },
  })
  const mounted = mount(component)
  t.after(() => mounted.unmount())
  await settle()
  const opener = doc.querySelector('button[aria-label="Expand sidebar"]')!
  assert.ok(opener)
  opener.focus()
  await send(opener, 'click')
  const dialog = doc.querySelector('[role="dialog"]')!
  assert.ok(dialog)
  assert.ok(String(dialog.props.class).includes('rq-dialog--drawer-left'))
  const last = doc.getElementById('mobile-last')!
  last.focus()
  await send(last, 'keydown', { key: 'Tab' })
  assert.ok(dialog.contains(doc.activeElement))
  await send(last, 'keydown', { key: 'Escape' })
  assert.equal(doc.querySelector('[role="dialog"]'), null)
  assert.equal(doc.activeElement, opener)
  await send(opener, 'click')
  route.fullPath = '/platform/topics'
  await settle()
  assert.equal(doc.querySelector('[role="dialog"]'), null)
  await send(opener, 'click')
  commands.openPalette()
  await settle()
  assert.equal(doc.querySelector('[role="dialog"]'), null)
  commands.open = false
  await send(opener, 'click')
  for (const listener of listeners) listener({ matches: false })
  await nextTick(); await settle()
  assert.equal(doc.querySelector('[role="dialog"]'), null)
  await mounted.unmount()
  assert.equal(listeners.size, 0)
})

test('shell constrains only its flex outlet and does not apply generic page styles', async () => {
  const rules = await compiledSFCStyleRules(new URL('./index.vue', import.meta.url))
  const outlet = styleDeclarationsAtWidth(rules, '.platform-route-outlet', 1280)
  assert.equal(outlet['min-width'], '0')
  assert.equal(outlet['min-height'], '0')
  assert.equal(outlet.overflow, 'hidden')
  assert.equal(outlet['flex-direction'], 'column')
  assert.equal(styleDeclarationsAtWidth(rules, '.rq-platform', 640)['flex-direction'], 'column')
  assert.equal(styleDeclarationsAtWidth(rules, '.rq-platform', 1280).height, '100%')
  assert.ok(!rules.some(rule => rule.selector === '.main' || rule.selector === 'img'))
})
