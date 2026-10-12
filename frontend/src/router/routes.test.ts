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
    assert.equal(router.currentRoute.value.name, 'entries', path)
    assert.equal(router.currentRoute.value.fullPath, '/platform/entries', path)
    assert.equal(notices, previousNotices + 1, path)
    notifyRemovedFeatureRedirect(router.currentRoute.value, () => { notices++ })
    assert.equal(notices, previousNotices + 1, 'a completed redirect is announced once')
    await router.push('/platform/entries')
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
  assert.match(source, /router.push\('\/platform\/entries'\)/)
  assert.match(source, /t\('navigation.returnToKnowledge'\)/)
})

test('settings deep links and system shortcuts mount one settings instance', async () => {
  const routes = createApplicationRoutes()
  const platform = routes.find(route => route.name === 'Platform')!
  const settings = platform.children!.find(route => route.name === 'settings')!
  const list = platform.children!.find(route => route.name === 'entries')!
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
  for (const path of ['/platform/settings?section=space', '/platform/settings?section=runtime-queues', '/platform/system/queues']) {
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

test('login and registration return to knowledge bases and removed conversation pages show not found', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const path of ['/', '/platform']) {
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'entries')
  }
  for (const path of ['/platform/chat/old-id', '/platform/creatChat', '/platform/agents', '/platform/entries/obsolete']) {
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'platformNotFound', path)
  }
  const login = readFileSync(new URL('../views/auth/Login.vue', import.meta.url), 'utf8')
  assert.equal((login.match(/router\.replace\('\/platform\/entries'\)/g) || []).length, 2)
  const navigation = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
  assert.match(navigation, /to\.path === '\/login' && authStore\.isLoggedIn[\s\S]*next\('\/platform\/entries'\)/)
})

test('old knowledge list and ID routes redirect to the single article page', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const old of ['/platform/knowledge-bases', '/platform/knowledge-bases/kb-a', '/platform/knowledge-bases/kb-a/documents']) {
    await router.push(old + '?entry_id=article-a')
    assert.equal(router.currentRoute.value.path, '/platform/entries')
    assert.equal(router.currentRoute.value.query.entry_id, 'article-a')
    assert.deepEqual(router.currentRoute.value.params, {})
  }
})

test('knowledge search has an independent page and preserves query and filter URLs', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  await router.push('/platform/search?q=Launch&sources=article&collapse_similar=false')
  assert.equal(router.currentRoute.value.name, 'search')
  assert.equal(router.currentRoute.value.query.q, 'Launch')
  assert.equal(router.currentRoute.value.query.sources, 'article')
  assert.equal(router.currentRoute.value.query.collapse_similar, 'false')
})

test('the operation log bookmark opens its settings section and preserves filters', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  await router.push('/platform/operation-logs?operation_type=star&page=2')
  assert.equal(router.currentRoute.value.name, 'settings')
  assert.deepEqual(router.currentRoute.value.query, { section: 'operation-logs', operation_type: 'star', page: '2' })
})

test('saved list and search bookmarks redirect with query and hash intact', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const [oldPath, newPath] of [['/platform/knowledge', '/platform/entries'], ['/platform/knowledge-search', '/platform/search']]) {
    await router.push(`${oldPath}?query=observations&sources=mine#selected`)
    assert.equal(router.currentRoute.value.path, newPath)
    assert.deepEqual(router.currentRoute.value.query, { query: 'observations', sources: 'mine' })
    assert.equal(router.currentRoute.value.hash, '#selected')
  }
})

test('a pre-rename bookmarked entry selection reaches the current detail query', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  await router.push('/platform/knowledge?knowledge_id=original-entry#detail')
  assert.equal(router.currentRoute.value.path, '/platform/entries')
  assert.deepEqual(router.currentRoute.value.query, { entry_id: 'original-entry' })
  assert.equal(router.currentRoute.value.hash, '#detail')
})

test('old collection page URLs redirect to the article list instead of not found', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const path of ['/platform/datasource', '/platform/data-sources/old-source', '/platform/sync-logs/old-log', '/platform/import', '/platform/upload', '/platform/manual']) {
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'entries', path)
    assert.equal(router.currentRoute.value.fullPath, '/platform/entries', path)
    assert.deepEqual(router.currentRoute.value.params, {})
  }
})
