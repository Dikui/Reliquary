import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test, { type TestContext } from 'node:test'
import axios, { AxiosError, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { createPinia, defineStore, setActivePinia } from 'pinia'
import { ref } from 'vue'
import ts from 'typescript'
import * as timestamps from '../utils/rfc3339'
import { translatePersonalServerMessage } from '../utils/personalServerError'

const require = createRequire(import.meta.url)
function evaluateFile(path: string, modules: Record<string, unknown>) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), value, value.exports)
  return value.exports
}
const envelope = (data: unknown) => ({ success: true, data })
const target = (type = 'entity', id = 'entity-a', name = 'Aurora') => ({ target_type: type, target_id: id, name, created_at: '2026-10-01T00:00:00.123456Z' })
type PendingRequest = {
  config: InternalAxiosRequestConfig
  finished: boolean
  claimed: boolean
  resolve: (data: unknown) => void
  reject: (status: number, message: string) => void
}

function fixture(t: TestContext) {
  const storage = new Map([['locale', 'zh-CN']])
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  } })
  const requests: PendingRequest[] = []
  const transport = { deferState: false, state: { new_count: 0, last_viewed_at: null as string | null } }
  const network = { __esModule: true, default: { create: (config: object) => axios.create({ ...config, adapter: (config: InternalAxiosRequestConfig) => new Promise<AxiosResponse>((resolve, reject) => {
    const packet: PendingRequest = { config, finished: false, claimed: false,
      resolve: data => { packet.finished = true; resolve({ data, status: 200, statusText: 'OK', headers: {}, config }) },
      reject: (status, message) => { packet.finished = true; const response = { data: { success: false, error: { message } }, status, statusText: 'Unavailable', headers: {}, config }; reject(new AxiosError(message, AxiosError.ERR_BAD_RESPONSE, config, undefined, response)) },
    }
    requests.push(packet)
    if (config.url === '/api/v1/follows/state' && !transport.deferState) packet.resolve(envelope(transport.state))
  }) }) } }
  const request = evaluateFile('../utils/request.ts', {
    axios: network,
    './index': { generateRandomString: () => 'request-fixture', MAX_FILE_SIZE_MB: 100 },
    './personalServerError': { translatePersonalServerMessage },
    './api-base': { getApiBaseUrl: () => 'http://follows.test' },
    '@/i18n': { __esModule: true, default: { global: { locale: { value: 'zh-CN' }, t: (key: string) => key } } },
  })
  const api = evaluateFile('../api/follows.ts', { '@/utils/request': request })
  setActivePinia(createPinia())
  const store = evaluateFile('./follows.ts', { pinia: { defineStore }, vue: { ref }, '@/api/follows': api, '@/utils/rfc3339': timestamps }).useFollowStore()
  t.after(async () => {
    request.stopServiceAvailabilityRetry()
    for (const packet of requests) if (!packet.finished) packet.resolve(envelope(packet.config.url === '/api/v1/follows' ? [] : transport.state))
    await new Promise<void>(resolve => setImmediate(resolve))
    storage.clear()
  })
  async function next(method: string, url: string): Promise<PendingRequest> {
    for (let attempt = 0; attempt < 20; attempt++) {
      const packet = requests.find(packet => !packet.finished && !packet.claimed && packet.config.method === method && packet.config.url === url)
      if (packet) { packet.claimed = true; return packet }
      await new Promise<void>(resolve => setImmediate(resolve))
    }
    assert.fail(`missing ${method} ${url}`)
  }
  async function load(targets: unknown[] = []) {
    const loading = store.loadTargets()
    const packet = await next('get', '/api/v1/follows')
    packet.resolve(envelope(targets))
    await loading
  }
  return { store, requests, transport, next, load }
}

test('follow targets load through the real API and authenticated Axios wrapper, coalesce and refresh', async t => {
  const f = fixture(t)
  const first = f.store.loadTargets(), second = f.store.loadTargets(true)
  assert.equal(f.store.targetsLoading, true)
  const packet = await f.next('get', '/api/v1/follows')
  assert.equal(packet.config.baseURL, 'http://follows.test')
  assert.equal(packet.config.headers.Authorization, undefined)
  assert.equal(packet.config.headers['Accept-Language'], 'zh-CN')
  packet.resolve(envelope([target(), target('story', 'story-a', 'Space event')]))
  await Promise.all([first, second])
  assert.equal(f.requests.filter(item => item.config.url === '/api/v1/follows').length, 1)
  assert.equal(f.store.targetsLoading, false)
  assert.equal(f.store.targetsLoaded, true)
  assert.equal(f.store.targetsError, false)
  assert.equal(f.store.isFollowing('entity', 'entity-a'), true)
  assert.equal(f.store.isFollowing('story', 'story-a'), true)
  assert.equal(f.store.isFollowing('story', 'entity-a'), false)
  await f.store.loadTargets()
  assert.equal(f.requests.length, 1)
  const refreshed = f.store.loadTargets(true)
  ;(await f.next('get', '/api/v1/follows')).resolve(envelope([target('entity', 'entity-a', 'Renamed Aurora')]))
  await refreshed
  assert.equal(f.store.targets[0].name, 'Renamed Aurora')
  assert.equal(f.store.isFollowing('story', 'story-a'), false)
})

test('failed initial loading has a retry path and never presents unknown targets as loaded', async t => {
  const f = fixture(t)
  const loading = f.store.loadTargets()
  const failed = assert.rejects(loading, (error: any) => error.status === 503 && error.message === 'Follow list unavailable')
  ;(await f.next('get', '/api/v1/follows')).reject(503, 'Follow list unavailable')
  await failed
  assert.equal(f.store.targetsLoaded, false)
  assert.equal(f.store.targetsError, true)
  assert.equal(f.store.targetsLoading, false)
  await f.load([target()])
  assert.equal(f.store.targetsLoaded, true)
  assert.equal(f.store.targetsError, false)
  assert.equal(f.store.isFollowing('entity', 'entity-a'), true)
})

test('follow and unfollow wait for server confirmation and prevent duplicate mutations', async t => {
  const f = fixture(t)
  await f.load()
  const following = f.store.setFollowing('entity', 'entity-a', 'Aurora', true)
  const packet = await f.next('put', '/api/v1/follows/entity/entity-a')
  assert.equal(packet.config.data, '{}')
  assert.equal(f.store.isPending('entity', 'entity-a'), true)
  assert.equal(f.store.isFollowing('entity', 'entity-a'), false)
  await f.store.setFollowing('entity', 'entity-a', 'Aurora', true)
  assert.equal(f.requests.filter(item => item.config.method === 'put').length, 1)
  packet.resolve(envelope(target()))
  await following
  await new Promise<void>(resolve => setImmediate(resolve))
  assert.equal(f.store.isFollowing('entity', 'entity-a'), true)
  assert.equal(f.store.isPending('entity', 'entity-a'), false)
  assert.equal(f.store.targets[0].created_at, target().created_at)
  const unfollowing = f.store.setFollowing('entity', 'entity-a', 'Aurora', false)
  const remove = await f.next('delete', '/api/v1/follows/entity/entity-a')
  assert.equal(f.store.isFollowing('entity', 'entity-a'), true)
  remove.resolve({ success: true })
  await unfollowing
  await new Promise<void>(resolve => setImmediate(resolve))
  assert.equal(f.store.isFollowing('entity', 'entity-a'), false)
  assert.equal(f.store.isPending('entity', 'entity-a'), false)
})

test('failed follow and unfollow preserve confirmed targets and allow a retry', async t => {
  const f = fixture(t)
  await f.load([target('story', 'story-a', 'Space event')])
  for (const [type, id, name, following, method] of [
    ['entity', 'entity-a', 'Aurora', true, 'put'],
    ['story', 'story-a', 'Space event', false, 'delete'],
  ] as const) {
    const mutation = f.store.setFollowing(type, id, name, following)
    const rejected = assert.rejects(mutation, (error: any) => error.status === 503)
    ;(await f.next(method, `/api/v1/follows/${type}/${id}`)).reject(503, 'Follow mutation unavailable')
    await rejected
    assert.equal(f.store.isFollowing(type, id), !following)
    assert.equal(f.store.isPending(type, id), false)
    const retry = f.store.setFollowing(type, id, name, following)
    ;(await f.next(method, `/api/v1/follows/${type}/${id}`)).resolve(following ? envelope(target(type, id, name)) : { success: true })
    await retry
    assert.equal(f.store.isFollowing(type, id), following)
  }
})

test('a late follow list snapshot cannot overwrite a confirmed follow or unfollow', async t => {
  const f = fixture(t)
  await f.load()
  const following = f.store.setFollowing('entity', 'entity-a', 'Aurora', true)
  const mutation = await f.next('put', '/api/v1/follows/entity/entity-a')
  const reading = f.store.loadTargets(true)
  const staleList = await f.next('get', '/api/v1/follows')
  mutation.resolve(envelope(target()))
  await following
  staleList.resolve(envelope([]))
  await reading
  assert.equal(f.store.isFollowing('entity', 'entity-a'), true)
  const unfollowing = f.store.setFollowing('entity', 'entity-a', 'Aurora', false)
  const removal = await f.next('delete', '/api/v1/follows/entity/entity-a')
  const anotherReading = f.store.loadTargets(true)
  const staleFollowed = await f.next('get', '/api/v1/follows')
  removal.resolve({ success: true })
  await unfollowing
  staleFollowed.resolve(envelope([target()]))
  await anotherReading
  assert.equal(f.store.isFollowing('entity', 'entity-a'), false)
})

test('following a merged story ID deduplicates the canonical target returned by the server', async t => {
  const f = fixture(t)
  const retained = target('story', 'retained', 'Retained story')
  await f.load([retained])
  const mutation = f.store.setFollowing('story', 'merged', 'Old story', true)
  ;(await f.next('put', '/api/v1/follows/story/merged')).resolve(envelope(retained))
  await mutation
  assert.equal(f.store.targets.length, 1)
  assert.equal(f.store.isFollowing('story', 'retained'), true)
  assert.equal(f.store.isFollowing('story', 'merged'), false)
})
