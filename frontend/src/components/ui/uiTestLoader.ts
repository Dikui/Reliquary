import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { loadComponent } from '../../views/library/componentTestHarness.ts'

const cache = new Map<string, any>()

export function loadUiComponent(name: string): any {
  if (cache.has(name)) return cache.get(name)
  const url = new URL(`./${name}.vue`, import.meta.url)
  const require = createRequire(url)
  const modules: Record<string, unknown> = {}
  for (const [, path] of readFileSync(url, 'utf8').matchAll(/from ['"]([^'"]+)['"]/g)) {
    if (path!.endsWith('.vue')) modules[path!] = { __esModule: true, default: loadUiComponent(path!.replace('./', '').replace('.vue', '')) }
    else if (path!.startsWith('.')) modules[path!] = require(path!)
  }
  const component = loadComponent(url, modules)
  cache.set(name, component)
  return component
}
