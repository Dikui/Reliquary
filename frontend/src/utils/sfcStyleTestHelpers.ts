import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'

export const LONG_SOURCE_URL = 'http://127.0.0.1:18091/samples/orion-original/'
  + 'LongSourceSegment0123456789'.repeat(8)
  + '?review=' + 'abcdefghij'.repeat(12)

export type CompiledStyleRule = {
  selector: string
  media: string[]
  containers: string[]
  declarations: Record<string, string>
}

// Inspect production Less after scoped selector compilation. This does not model
// DOM geometry or replace browser verification of the resulting layout.
export async function compiledSFCStyleRules(url: URL): Promise<CompiledStyleRule[]> {
  const filename = fileURLToPath(url)
  const source = readFileSync(url, 'utf8')
  const descriptor = filename.endsWith('.css') ? { styles: [{ content: source, scoped: false, lang: undefined }] } : parse(source, { filename }).descriptor
  const rules: CompiledStyleRule[] = []
  for (const style of descriptor.styles) {
    const compiled = await compileStyleAsync({
      filename, id: 'data-v-layout-test', source: style.content, scoped: style.scoped,
      preprocessLang: style.lang === 'less' ? 'less' : undefined,
    })
    assert.deepEqual(compiled.errors, [], 'the production Less and scoped CSS compile successfully')
    compiled.rawResult!.root.walkRules(rule => {
      const media: string[] = []
      const containers: string[] = []
      let parent: any = rule.parent
      while (parent) {
        if (parent.type === 'atrule' && parent.name === 'media') media.push(parent.params)
        if (parent.type === 'atrule' && parent.name === 'container') containers.push(parent.params)
        parent = parent.parent
      }
      const declarations: Record<string, string> = {}
      rule.walkDecls(declaration => { declarations[declaration.prop] = declaration.value })
      for (const selector of rule.selector.split(',')) {
        rules.push({ selector: selector.replace(/\[data-v-layout-test\]/g, '').trim(), media, containers, declarations })
      }
    })
  }
  return rules
}

function mediaAppliesAtWidth(media: string, width: number): boolean {
  if (/\bprint\b/.test(media)) return false
  const bounds = [...media.matchAll(/\((min|max)-width:\s*(\d+)px\)/g)]
  if (!bounds.length) return !media || media === 'screen' || media === 'all'
  return bounds.every(([, kind, value]) => kind === 'max' ? width <= Number(value) : width >= Number(value))
}

// Merge matching selector declarations in stylesheet order at a screen width.
export function styleDeclarationsAtWidth(rules: CompiledStyleRule[], selector: string, width: number, containerWidths: Record<string, number> = {}): Record<string, string> {
  return Object.assign({}, ...rules.filter(rule => rule.selector === selector
    && rule.media.every(media => mediaAppliesAtWidth(media, width))
    && rule.containers.every(container => {
      const name = container.match(/^([\w-]+)\s+/)?.[1]
      return name !== undefined && containerWidths[name] !== undefined
        && mediaAppliesAtWidth(container.slice(name.length), containerWidths[name])
    })).map(rule => rule.declarations))
}
