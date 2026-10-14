import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createRenderer, defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./ReportAssociations.vue', import.meta.url), 'utf8')

function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)(
    (name: string) => modules[name] ?? require(name), module, module.exports,
  )
  return module.exports
}

function component(batchQueryEntry: (query: string) => Promise<unknown>) {
  const { descriptor, errors } = parse(source, { filename: 'ReportAssociations.vue' })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: 'report-associations-test' })
  const template = compileTemplate({
    id: 'report-associations-test',
    filename: 'ReportAssociations.vue',
    source: descriptor.template!.content,
    compilerOptions: { bindingMetadata: script.bindings },
  })
  assert.deepEqual(template.errors, [])
  const value = evaluate(script.content, { '@/api/library': { batchQueryEntry } }).default
  value.render = evaluate(template.code, {}).render
  return value
}

type HostNode = {
  tag?: string
  text?: string
  children: HostNode[]
  parent?: HostNode
  props?: Record<string, any>
}

const node = (tag?: string): HostNode => ({ tag, children: [] })
const renderer = createRenderer<HostNode, HostNode>({
  querySelector: () => null,
  createElement: tag => node(tag),
  createText: text => ({ ...node(), text }),
  createComment: () => node('comment'),
  setText: (el, text) => { el.text = text },
  setElementText: (el, text) => { el.text = text },
  patchProp: (el, key, _old, value) => { (el.props ||= {})[key] = value },
  insert(el, parent, anchor) {
    if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1)
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, el)
    el.parent = parent
  },
  remove(el) {
    if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1)
  },
  parentNode: el => el.parent || null,
  nextSibling: el => el.parent?.children[el.parent.children.indexOf(el) + 1] || null,
  insertStaticContent(text, parent) {
    const el = { ...node(), text, parent }
    parent.children.push(el)
    return [el, el]
  },
})

const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const flatten = (value: HostNode): HostNode[] => [value, ...value.children.flatMap(flatten)]
const textContent = (value: HostNode): string => `${value.text || ''}${value.children.map(textContent).join('')}`
const hasClass = (value: HostNode, className: string) => String(value.props?.class || '').split(' ').includes(className)

async function mount(
  locale: keyof typeof messages,
  props: Record<string, unknown>,
  batchQueryEntry: (query: string) => Promise<unknown>,
) {
  const root = node('root')
  const errors: unknown[] = []
  const warnings: string[] = []
  const app = renderer.createApp(component(batchQueryEntry), props)
  app.use(createI18n({ legacy: false, locale, messages }))
  app.component('t-loading', defineComponent({ setup: () => () => h('span') }))
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  app.mount(root)
  await nextTick()
  await new Promise<void>(resolve => setImmediate(resolve))
  await nextTick()
  assert.deepEqual(errors, [])
  assert.deepEqual(warnings, [])
  return { app, root, nodes: () => flatten(root) }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: related material titles and libraries render in report order and clicks open each target`, async () => {
    const queries: string[] = []
    const openedEntries: string[] = []
    const openedEntities: string[] = []
    const openedTopics: string[] = []
    const view = await mount(locale, {
      entryIds: ['entry-2', 'entry-1'],
      entities: [{ id: 'entity-1', name: 'Atlas Lab' }],
      topics: [{ id: 'topic-1', name: 'Calibration' }],
      onOpenEntry: (id: string) => openedEntries.push(id),
      onOpenEntity: (id: string) => openedEntities.push(id),
      onOpenTopic: (id: string) => openedTopics.push(id),
    }, async query => {
      queries.push(query)
      return { success: true, data: [
        { id: 'entry-1', title: 'First source', library_name: 'References' },
        { id: 'entry-2', title: 'Second source', library_name: 'Institutional releases' },
      ] }
    })
    try {
      const text = textContent(view.root)
      assert.ok(text.indexOf('Second source') < text.indexOf('First source'))
      assert.ok(text.includes('Institutional releases'))
      assert.ok(text.includes('References'))
      assert.equal(new URLSearchParams(queries[0]).getAll('ids').join(','), 'entry-2,entry-1')

      const links = view.nodes().filter(value => value.tag === 'button' && hasClass(value, 'association-link'))
      assert.equal(links.length, 2)
      links[0]!.props!.onClick()
      const tags = view.nodes().filter(value => value.tag === 'button' && hasClass(value, 'association-tag'))
      tags[0]!.props!.onClick()
      tags[1]!.props!.onClick()
      assert.deepEqual(openedEntries, ['entry-2'])
      assert.deepEqual(openedEntities, ['entity-1'])
      assert.deepEqual(openedTopics, ['topic-1'])
    } finally {
      view.app.unmount()
    }
  })
}

test('an empty report association list renders no section and makes no request', async () => {
  let calls = 0
  const view = await mount('zh-CN', { entryIds: [], entities: [], topics: [] }, async () => {
    calls++
    return { success: true, data: [] }
  })
  try {
    assert.equal(calls, 0)
    assert.equal(view.nodes().some(value => 'data-report-associations' in (value.props || {})), false)
  } finally {
    view.app.unmount()
  }
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: a deleted or unreadable related material keeps a localized placeholder`, async () => {
    const view = await mount(locale, { entryIds: ['present', 'missing'] }, async () => ({
      success: true,
      data: [{ id: 'present', title: 'Available source', library_name: 'My materials' }],
    }))
    try {
      assert.ok(textContent(view.root).includes(messages[locale].library.relatedEntryUnavailable))
      assert.equal(view.nodes().filter(value => value.tag === 'button' && hasClass(value, 'association-link')).length, 1)
      assert.equal(view.nodes().filter(value => hasClass(value, 'association-unavailable')).length, 1)
    } finally {
      view.app.unmount()
    }
  })
}
