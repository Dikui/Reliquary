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

test('material details have a direct route and legacy bookmarks preserve references', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  await router.push('/platform/entries/article-a?reference=paragraph&tab=relations')
  assert.equal(router.currentRoute.value.name, 'entryDetail')
  assert.equal(router.currentRoute.value.params.id, 'article-a')
  await router.push('/platform/entries?entry_id=article-b&reference=paragraph&libraries=lib-a')
  assert.equal(router.currentRoute.value.name, 'entryDetail')
  assert.equal(router.currentRoute.value.params.id, 'article-b')
  assert.deepEqual(router.currentRoute.value.query, { reference: 'paragraph', libraries: 'lib-a' })
})

test('removed feature routes return to libraries with one notice', async () => {
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
  assert.match(source, /t\('navigation.returnToEntries'\)/)
})

test('settings deep links and system shortcuts mount one settings instance', async () => {
  const routes = createApplicationRoutes()
  const platform = routes.find(route => route.name === 'Platform')!
  const settings = platform.children!.find(route => route.name === 'settings')!
  const list = platform.children!.find(route => route.name === 'libraries')!
  assert.notEqual('component' in settings && settings.component, 'component' in list && list.component)

  const source = readFileSync(new URL('../views/platform/index.vue', import.meta.url), 'utf8')
  assert.equal((source.match(/<Settings\s*\/>/g) || []).length, 0)

  const renderedRoutes = testRoutes()
  const renderedPlatform = renderedRoutes.find(route => route.name === 'Platform')!
  let settingsMounts = 0
  const settingsStub = defineComponent({ setup() { settingsMounts++; return () => h('aside', { 'data-settings-instance': '' }) } })
  if ('component' in renderedPlatform) {
    renderedPlatform.component = defineComponent({ render: () => h('main', [h(RouterView)]) })
  }
  const renderedSettings = renderedPlatform.children!.find(route => route.name === 'settings')!
  if ('component' in renderedSettings) renderedSettings.component = settingsStub
  for (const path of ['/platform/settings?section=space', '/platform/settings?section=runtime-queues', '/platform/system/queues']) {
    settingsMounts = 0
    const router = createRouter({ history: createMemoryHistory(), routes: renderedRoutes })
    await router.push(path)
    const app = createSSRApp({ render: () => h(RouterView) })
    app.use(router)
    const html = await renderToString(app)
    assert.equal(settingsMounts, 1, path)
    assert.equal((html.match(/data-settings-instance/g) || []).length, 1, path)
    assert.doesNotMatch(html, /data-page="libraries"/, path)
  }
})

test('root, platform and old login links return to libraries and removed conversation pages show not found', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const path of ['/', '/platform', '/login', '/platform/libraries']) {
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'libraries')
  }
  for (const path of ['/platform/chat/old-id', '/platform/creatChat', '/platform/agents']) {
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'platformNotFound', path)
  }
})

test('old library ID routes keep their library scope and entry reference', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const old of ['/platform/libraries/library-a', '/platform/libraries/library-a/documents']) {
    await router.push(old + '?entry_id=article-a')
    assert.equal(router.currentRoute.value.path, '/platform/entries/article-a')
    assert.equal(router.currentRoute.value.params.id, 'article-a')
    assert.equal(router.currentRoute.value.query.libraries, 'library-a')
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

test('retired entry route aliases are no longer registered', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const path of ['/platform/knowledge', '/platform/knowledge-search']) {
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'platformNotFound')
  }
  const source = readFileSync(new URL('./routes.ts', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /knowledge_id/)
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

 test('retired library URLs have no redirect aliases', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const path of ['/platform/knowledge-bases', '/platform/knowledge-bases/old/documents']) {
    await router.push(path)
    assert.equal(router.currentRoute.value.name, 'platformNotFound', path)
  }
})

test('entities use an independent route while legacy entity bookmarks preserve context', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  await router.push('/platform/entities?entity_id=company&libraries=lib-a')
  assert.equal(router.currentRoute.value.name, 'entities')
  await router.push('/platform/entries?tab=entities&entity_id=company&libraries=lib-a#profile')
  assert.equal(router.currentRoute.value.name, 'entities')
  assert.equal(router.currentRoute.value.query.entity_id, 'company')
  assert.equal(router.currentRoute.value.query.libraries, 'lib-a')
  assert.equal(router.currentRoute.value.hash, '#profile')
  await router.push('/platform/entries?tab=entities&section=merges&libraries=lib-a')
  assert.equal(router.currentRoute.value.name, 'pending')
  assert.equal(router.currentRoute.value.query.tab, 'merges')
})


test('pending bookmarks redirect with their tab, library scope and reference intact', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: testRoutes() })
  for (const [legacy, tab] of [['reviews', 'reviews'], ['entities&section=merges', 'merges']]) {
    await router.push(`/platform/entries?tab=${legacy}&libraries=a,b&entry_id=entry-a#evidence`)
    assert.equal(router.currentRoute.value.name, 'pending')
    assert.deepEqual(router.currentRoute.value.query, { tab, libraries: 'a,b', entry_id: 'entry-a' })
    assert.equal(router.currentRoute.value.hash, '#evidence')
  }
  await router.push('/platform/pending')
  assert.equal(router.currentRoute.value.name, 'pending')
})
