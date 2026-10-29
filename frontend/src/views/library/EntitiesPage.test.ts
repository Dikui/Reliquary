import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import * as vueRouter from 'vue-router'
import { entryDetailLocation } from '../../utils/bodyReference'
import { click, descendants, element, loadComponent, renderer, settle } from './componentTestHarness'

test('retained entity selection replaces the merged ID in the URL and preserves the library', async t => {
  const router = vueRouter.createRouter({ history: vueRouter.createMemoryHistory(), routes: [
    { path: '/platform/entities', component: { render: () => null } },
  ] })
  await router.push('/platform/entities?entity_id=merged&libraries=library-a')
  const component = loadComponent(new URL('./EntitiesPage.vue', import.meta.url), {
    'vue-router': vueRouter,
    '@/utils/bodyReference': { entryDetailLocation },
    './components/EntryWorkbench.vue': { __esModule: true, default: defineComponent({
      props: ['initialEntityId'], emits: ['select-entity'],
      setup: (props, { emit }) => () => h('button', { onClick: () => emit('select-entity', 'retained', 'library-a') }, props.initialEntityId),
    }) },
  })
  const app = renderer.createApp(component); app.use(router)
  const root = element('root'); app.mount(root); t.after(() => app.unmount())
  await click(descendants(root).find(node => node.tag === 'button')!)
  await new Promise<void>(resolve => { setImmediate(resolve) }); await settle()
  assert.equal(router.currentRoute.value.path, '/platform/entities')
  assert.deepEqual(router.currentRoute.value.query, { entity_id: 'retained', libraries: 'library-a' })
  assert.equal(descendants(root).find(node => node.tag === 'button')!.text, 'retained')
})
