import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { buildCommands } from './commands'

test('commands and sidebar expose knowledge and settings navigation', () => {
  const paths: string[] = []
  let closes = 0
  const commands = buildCommands({ router: { push: (path: string) => paths.push(path) } as any, t: ((key: string) => key) as any, close: () => closes++ })
  assert.deepEqual(commands.map(command => command.id), ['open-knowledge', 'open-settings'])
  for (const command of commands) command.run()
  assert.deepEqual(paths, ['/platform/knowledge', '/platform/settings'])
  assert.equal(closes, 2)
  const sidebar = readFileSync(new URL('../menu.vue', import.meta.url), 'utf8')
  const menu = readFileSync(new URL('../../stores/menu.ts', import.meta.url), 'utf8')
  const palette = readFileSync(new URL('../GlobalCommandPalette.vue', import.meta.url), 'utf8')
  for (const source of [sidebar, menu, palette]) assert.doesNotMatch(source, /menu\.(?:agents|newChat|newSession|myChats)|commandPalette\.(?:quick\.(?:newChat|agents)|group\.(?:agents|messages|sessionsByTitle))/)
  assert.match(sidebar, /commandPaletteStore\.openPalette/)
  assert.match(menu, /knowledge/)
  assert.match(palette, /flatChunkItems\.value/)
})
