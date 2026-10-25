import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import ts from 'typescript'
import { buildCommands } from './commands'

test('commands and sidebar expose knowledge and settings navigation', () => {
  const paths: string[] = []
  let closes = 0
  const commands = buildCommands({ router: { push: (path: string) => paths.push(path) } as any, t: ((key: string) => key) as any, close: () => closes++ })
  assert.deepEqual(commands.map(command => command.id), ['open-entry', 'open-settings'])
  for (const command of commands) command.run()
  assert.deepEqual(paths, ['/platform/entries', '/platform/settings'])
  assert.equal(closes, 2)
  const sidebar = readFileSync(new URL('../SideMenu.vue', import.meta.url), 'utf8')
  const menu = readFileSync(new URL('../../stores/menu.ts', import.meta.url), 'utf8')
  const palette = readFileSync(new URL('../GlobalCommandPalette.vue', import.meta.url), 'utf8')
  for (const source of [sidebar, menu, palette]) assert.doesNotMatch(source, /menu\.(?:agents|newChat|newSession|myChats)|commandPalette\.(?:quick\.(?:newChat|agents)|group\.(?:agents|messages|sessionsByTitle))/)
  assert.match(menu, /path: 'search'/)
  assert.match(menu, /entries/)
  assert.match(palette, /flatChunkItems\.value/)
})

test('primary navigation includes evaluation and model calls in its established order', () => {
  const source = readFileSync(new URL('../../stores/menu.ts', import.meta.url), 'utf8')
  const module = { exports: {} as any }
  const require = createRequire(import.meta.url)
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  new Function('require', 'module', 'exports', compiled)((name: string) => {
    if (name === 'pinia') return { defineStore: (_name: string, setup: Function) => setup }
    if (name === '@/i18n') return { default: { global: { t: (key: string) => key } }, __esModule: true }
    return require(name)
  }, module, module.exports)
  const items = module.exports.useMenuStore().visibleMenuArr.value
  assert.deepEqual(items.map((item: any) => item.key), ['articles', 'stories', 'search', 'entities', 'domains', 'follows', 'pending', 'diagnostics', 'evaluation', 'modelCalls', 'settings'])
  assert.deepEqual(items.map((item: any) => [item.path, item.tab]), [
    ['entries', undefined], ['stories', undefined], ['search', undefined], ['entries', 'entities'], ['entries', 'domains'], ['follows', undefined], ['entries', 'reviews'], ['diagnostics', undefined], ['evaluation', undefined], ['model-calls', undefined], ['settings', undefined],
  ])
  const settings = readFileSync(new URL('../../views/settings/Settings.vue', import.meta.url), 'utf8')
  for (const section of ['library', 'models', 'vault', 'operation-logs', 'runtime-queues']) {
    assert.ok(settings.includes(`currentSection === '${section}'`), section)
    assert.ok(settings.includes(`key: '${section}'`), section)
  }
})
