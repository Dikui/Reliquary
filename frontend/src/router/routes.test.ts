import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createSSRApp, defineComponent, h } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createMemoryHistory, createRouter, RouterView, type RouteRecordRaw } from 'vue-router'
import { createApplicationRoutes } from './routes'
import { notifyRemovedFeatureRedirect } from './routeFallbacks'

function testRoutes(): RouteRecordRaw[] {
  const stub = (route: RouteRecordRaw): RouteRecordRaw => {
    const copy = { ...route }
    if ('component' in copy && copy.component) {
      copy.component = defineComponent({ render: () => h('div', { 'data-page': String(route.name || route.path) }) })
    }
    if (copy.children) copy.children = copy.children.map(stub)
    return copy as RouteRecordRaw
  }
  return createApplicationRoutes().map(stub)
}

test('removed feature routes return to knowledge bases with one notice', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  let notices = 0
  router.afterEach(to => notifyRemovedFeatureRedirect(to, () => { notices++ }))
  const paths = [
    '/platform/organizations',
    '/platform/organizations/old-id',
    '/platform/shared-spaces/old-id',
    '/join?code=obsolete',
    '/onboarding/workspace',
    '/embed/old-id',
    '/embed',
    '/embed.html',
  ]
  for (const path of paths) {
    await router.push('/login')
    const previousNotices = notices
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'knowledgeBaseList', path)
    assert.equal(router.currentRoute.value.fullPath, '/platform/knowledge-bases', path)
    assert.equal(notices, previousNotices + 1, path)
    notifyRemovedFeatureRedirect(router.currentRoute.value, () => { notices++ })
    assert.equal(notices, previousNotices + 1, 'a completed redirect is announced once')
    await router.push('/platform/knowledge-bases')
    assert.equal(notices, previousNotices + 1, 'a list refresh does not repeat the notice')
  }
})

test('unknown root and platform paths match an application error page', async () => {
  const routes = createApplicationRoutes()
  const platform = routes.find(route => route.name === 'Platform')!
  const fallback = routes.find(route => route.name === 'notFound')!
  const platformFallback = platform.children!.find(route => route.name === 'platformNotFound')!
  assert.equal('component' in fallback && fallback.component, 'component' in platformFallback && platformFallback.component)

  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  await router.push('/does-not-exist')
  assert.equal(router.currentRoute.value.name, 'notFound')
  await router.push('/platform/does-not-exist')
  assert.equal(router.currentRoute.value.name, 'platformNotFound')
  assert.equal(router.currentRoute.value.matched[0].name, 'Platform')
  assert.equal(router.currentRoute.value.meta.requiresAuth, true)

  const source = readFileSync(new URL('../views/NotFound.vue', import.meta.url), 'utf8')
  assert.match(source, /<h1>\{\{ t\('navigation.pageNotFound'\) \}\}<\/h1>/)
  assert.match(source, /router.push\('\/platform\/knowledge-bases'\)/)
  assert.match(source, /t\('navigation.returnToKnowledgeBases'\)/)
})

test('settings deep links and system shortcuts mount one settings instance', async () => {
  const routes = createApplicationRoutes()
  const platform = routes.find(route => route.name === 'Platform')!
  const settings = platform.children!.find(route => route.name === 'settings')!
  const list = platform.children!.find(route => route.name === 'knowledgeBaseList')!
  assert.equal('component' in settings && settings.component, 'component' in list && list.component)

  const source = readFileSync(new URL('../views/platform/index.vue', import.meta.url), 'utf8')
  assert.equal((source.match(/<Settings\s*\/>/g) || []).length, 1)

  const renderedRoutes = testRoutes()
  const renderedPlatform = renderedRoutes.find(route => route.name === 'Platform')!
  let settingsMounts = 0
  const settingsStub = defineComponent({ setup() { settingsMounts++; return () => h('aside', { 'data-settings-instance': '' }) } })
  if ('component' in renderedPlatform) {
    renderedPlatform.component = defineComponent({ render: () => h('main', [h(RouterView), h(settingsStub)]) })
  }
  for (const path of ['/platform/settings?section=tenant', '/platform/settings?section=runtime-queues', '/platform/system/queues']) {
    settingsMounts = 0
    const router = createRouter({ history: createMemoryHistory(), routes: renderedRoutes })
    await router.push(path)
    const app = createSSRApp({ render: () => h(RouterView) })
    app.use(router)
    const html = await renderToString(app)
    assert.equal(settingsMounts, 1, path)
    assert.equal((html.match(/data-settings-instance/g) || []).length, 1, path)
    assert.match(html, /data-page="settings"/, path)
  }
})
