import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createRenderer, defineComponent, h, nextTick, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
import type { WikiPage } from '../../../api/wiki'

const require = createRequire(import.meta.url)
const filename = 'WikiSourceChanges.vue'
const source = readFileSync(new URL('./WikiSourceChanges.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename })
const compiled = compileScript(descriptor, { id: 'wiki-source-changes-test', inlineTemplate: true })
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const changedAt = '2026-10-08T06:20:15Z'
const page = (overrides: Partial<WikiPage> = {}) => ({
  id: 'page-a', knowledge_base_id: 'kb-a', slug: 'concept/来源 页面', title: 'Source page',
  source_changes: { 'chunk-a': changedAt, 'chunk-b': '2026-10-08T07:00:00Z' }, ...overrides,
} as WikiPage)

interface Element {
  tag: string
  text: string
  props: Record<string, any>
  children: Element[]
  parent: Element | null
}
const element = (tag: string, text = ''): Element => ({ tag, text, props: {}, children: [], parent: null })
const renderer = createRenderer<Element, Element>({
  createElement: tag => element(tag),
  createText: value => element('#text', value),
  createComment: value => element('#comment', value),
  setText(node, value) { node.text = value },
  setElementText(node, value) { node.children = []; node.text = value },
  parentNode: node => node.parent,
  nextSibling(node) {
    const siblings = node.parent?.children || []
    return siblings[siblings.indexOf(node) + 1] || null
  },
  insert(node, parent, anchor = null) {
    if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
    node.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    if (index < 0) parent.children.push(node)
    else parent.children.splice(index, 0, node)
  },
  remove(node) {
    if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
    node.parent = null
  },
  patchProp(node, key, _oldValue, value) { node.props[key] = value },
})
const descendants = (node: Element): Element[] => [node, ...node.children.flatMap(descendants)]
const visibleText = (node: Element): string => node.tag === '#comment' ? '' : node.text + node.children.map(visibleText).join('')

function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (error: unknown) => void
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

function mountNotice({ locale = 'zh-CN', initialPage = page(), canEdit = true, busy = false, editing = false,
  ignore = async () => ({ data: page({ source_changes: {} }) }), regenerate = async () => ({ data: page() }),
}: {
  locale?: keyof typeof messages; initialPage?: WikiPage; canEdit?: boolean; busy?: boolean; editing?: boolean
  ignore?: (kbId: string, slug: string) => Promise<any>
  regenerate?: (kbId: string, slug: string) => Promise<any>
} = {}) {
  const notices: Array<{ kind: string; message: string }> = []
  const updates: WikiPage[] = []
  const requests: Array<{ action: string; kbId: string; slug: string }> = []
  let queued = 0
  const modules: Record<string, unknown> = {
    'tdesign-vue-next': { MessagePlugin: {
      success: (message: string) => notices.push({ kind: 'success', message }),
      error: (message: string) => notices.push({ kind: 'error', message }),
    } },
    '@/api/wiki': {
      ignoreWikiSourceChanges: (kbId: string, slug: string) => {
        requests.push({ action: 'ignore', kbId, slug }); return ignore(kbId, slug)
      },
      regenerateWikiSourceChanges: (kbId: string, slug: string) => {
        requests.push({ action: 'regenerate', kbId, slug }); return regenerate(kbId, slug)
      },
    },
  }
  const module = { exports: {} as any }
  const code = ts.transpileModule(compiled.content, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  new Function('require', 'module', 'exports', code)(
    (name: string) => modules[name] ?? require(name), module, module.exports,
  )
  const props = reactive({ page: initialPage, kbId: 'kb-a', canEdit, busy, editing })
  const app = renderer.createApp({
    render: () => h(module.exports.default, {
      ...props,
      onUpdated: (updated: WikiPage) => { updates.push(updated); props.page = updated },
      onRegenerateQueued: () => { queued++ },
    }),
  })
  const i18n = createI18n({ legacy: false, locale, messages })
  app.use(i18n)
  app.component('t-icon', defineComponent({ setup: () => () => h('span') }))
  app.component('t-button', defineComponent({
    props: ['disabled', 'loading', 'size', 'theme', 'variant'],
    setup(buttonProps, { attrs, slots }) {
      return () => h('button', { ...attrs, type: 'button', disabled: !!buttonProps.disabled, 'data-loading': !!buttonProps.loading }, slots.default?.())
    },
  }))
  app.component('t-popconfirm', defineComponent({
    props: ['disabled', 'content'], emits: ['confirm'],
    setup(confirmProps, { slots, emit }) {
      const open = ref(false)
      return () => h('div', {
        'data-confirmation': true,
        onClick: () => { if (!confirmProps.disabled) open.value = true },
      }, [slots.default?.(), open.value ? h('div', { role: 'alertdialog' }, [
        h('p', confirmProps.content),
        h('button', { type: 'button', 'data-confirm': true, disabled: !!confirmProps.disabled, onClick: () => { open.value = false; emit('confirm') } }, 'Confirm'),
      ]) : null])
    },
  }))
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const root = element('root')
  app.mount(root)
  const button = (label: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)
  const click = async (node: Element) => {
    if (node.props.disabled) return
    if (node.props.onClick) node.props.onClick()
    else node.parent?.props.onClick?.()
    await nextTick()
  }
  const confirmRegenerate = async () => {
    await click(button(messages[locale].knowledgeEditor.wikiBrowser.sourceChangesRegenerate)!)
    const confirm = descendants(root).find(node => node.props['data-confirm'])
    assert.ok(confirm, 'regeneration requires the existing application confirmation')
    await click(confirm)
  }
  return { root, props, i18n, requests, notices, updates, errors, button, click, confirmRegenerate,
    queued: () => queued, unmount: () => app.unmount() }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale} renders the modified source marker, affected chunks and localized exact times`, () => {
    const notice = mountNotice({ locale })
    const copy = messages[locale].knowledgeEditor.wikiBrowser
    assert.ok(visibleText(notice.root).includes(copy.sourceChangesTitle))
    for (const id of ['chunk-a', 'chunk-b']) assert.ok(visibleText(notice.root).includes(id))
    const times = descendants(notice.root).filter(node => node.tag === 'time')
    assert.deepEqual(times.map(node => node.props.datetime), [changedAt, '2026-10-08T07:00:00Z'])
    assert.equal(visibleText(times[0]!), new Intl.DateTimeFormat(locale, {
      year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short',
    }).format(new Date(changedAt)))
    assert.ok(notice.button(copy.sourceChangesRegenerate))
    assert.ok(notice.button(copy.sourceChangesIgnore))
    assert.doesNotMatch(visibleText(notice.root), /knowledgeEditor\.wikiBrowser\./)
    assert.deepEqual(notice.errors, [])
    notice.unmount()
  })

  test(`${locale} Ignore submits the selected page and removes the marker only after success`, async () => {
    const pending = deferred()
    const notice = mountNotice({ locale, ignore: () => pending.promise })
    const copy = messages[locale].knowledgeEditor.wikiBrowser
    await notice.click(notice.button(copy.sourceChangesIgnore)!)
    assert.equal(notice.requests.length, 1)
    assert.ok(visibleText(notice.root).includes(copy.sourceChangesTitle))
    assert.ok(notice.button(copy.sourceChangesIgnore)!.props.disabled)
    assert.ok(notice.button(copy.sourceChangesIgnore)!.props['data-loading'])
    assert.ok(notice.button(copy.sourceChangesRegenerate)!.props.disabled)
    await notice.click(notice.button(copy.sourceChangesIgnore)!)
    assert.equal(notice.requests.length, 1, 'pending actions cannot dispatch twice')
    pending.resolve({ data: page({ source_changes: {} }) })
    await pending.promise
    await nextTick()
    assert.deepEqual(notice.requests, [{ action: 'ignore', kbId: 'kb-a', slug: 'concept/来源 页面' }])
    assert.equal(visibleText(notice.root), '')
    assert.deepEqual(notice.notices, [{ kind: 'success', message: copy.sourceChangesIgnored }])
    assert.equal(notice.updates.length, 1)
    notice.unmount()
  })

  test(`${locale} Regenerate confirms, waits in the existing queue and clears only on a refreshed successful page`, async () => {
    const pending = deferred()
    const notice = mountNotice({ locale, regenerate: () => pending.promise })
    const copy = messages[locale].knowledgeEditor.wikiBrowser
    await notice.confirmRegenerate()
    assert.ok(notice.button(copy.sourceChangesRegenerate)!.props.disabled)
    assert.ok(notice.button(copy.sourceChangesRegenerate)!.props['data-loading'])
    assert.ok(notice.button(copy.sourceChangesIgnore)!.props.disabled)
    pending.resolve({ data: page() })
    await pending.promise
    await nextTick()
    assert.equal(notice.queued(), 1)
    assert.equal(notice.updates.length, 0, 'accepting a queued job does not clear the marker')
    assert.ok(visibleText(notice.root).includes(copy.sourceChangesTitle))
    assert.ok(visibleText(notice.root).includes(copy.sourceChangesQueued))
    assert.deepEqual(notice.notices, [{ kind: 'success', message: copy.sourceChangesRegenerateQueued }])
    notice.props.page = page({ source_changes: {} })
    await nextTick()
    assert.equal(visibleText(notice.root), '')
    notice.unmount()
  })
}

for (const action of ['regenerate', 'ignore'] as const) {
  test(`${action} failure keeps the changed sources, shows an inline error and allows retry`, async () => {
    let failing = true
    const operation = async () => {
      if (failing) throw new Error('internal diagnostic')
      return { data: page({ source_changes: {} }) }
    }
    const notice = mountNotice({ [action]: operation })
    const copy = zhCN.knowledgeEditor.wikiBrowser
    const run = () => action === 'regenerate' ? notice.confirmRegenerate() : notice.click(notice.button(copy.sourceChangesIgnore)!)
    await run()
    assert.ok(visibleText(notice.root).includes(copy.sourceChangesTitle))
    const error = descendants(notice.root).find(node => node.props.role === 'alert')
    assert.ok(error)
    assert.ok(visibleText(error).includes(action === 'regenerate' ? copy.sourceChangesRegenerateFailed : copy.sourceChangesIgnoreFailed))
    assert.doesNotMatch(visibleText(notice.root), /internal diagnostic/)
    assert.equal(notice.button(copy.sourceChangesIgnore)!.props.disabled, false)
    assert.equal(notice.button(copy.sourceChangesRegenerate)!.props.disabled, false)
    failing = false
    await run()
    assert.equal(notice.requests.length, 2)
    assert.equal(notice.notices.at(-1)!.kind, 'success')
    notice.unmount()
  })
}

test('source changes added while regeneration runs remain marked and can be processed again', async () => {
  const notice = mountNotice()
  await notice.confirmRegenerate()
  notice.props.page = page({ source_changes: { 'chunk-c': '2026-10-08T08:00:00Z' } })
  await nextTick()
  assert.ok(visibleText(notice.root).includes('chunk-c'))
  assert.doesNotMatch(visibleText(notice.root), /chunk-a|chunk-b/)
  assert.equal(notice.button(zhCN.knowledgeEditor.wikiBrowser.sourceChangesRegenerate)!.props.disabled, false)
  notice.unmount()
})

test('late Ignore completion cannot replace a different selected page', async () => {
  const pending = deferred()
  const notice = mountNotice({ ignore: () => pending.promise })
  await notice.click(notice.button(zhCN.knowledgeEditor.wikiBrowser.sourceChangesIgnore)!)
  notice.props.page = page({ id: 'page-b', slug: 'concept/another', source_changes: { 'chunk-c': changedAt } })
  await nextTick()
  pending.resolve({ data: page({ source_changes: {} }) })
  await pending.promise
  await nextTick()
  assert.equal(notice.props.page.id, 'page-b')
  assert.equal(notice.updates.length, 0)
  assert.equal(notice.notices.length, 0)
  assert.ok(visibleText(notice.root).includes('chunk-c'))
  notice.unmount()
})

for (const unavailable of ['busy', 'editing'] as const) {
  test(`${unavailable} displays why mutations are unavailable and disables both controls`, () => {
    const notice = mountNotice({ [unavailable]: true })
    const copy = zhCN.knowledgeEditor.wikiBrowser
    assert.ok(visibleText(notice.root).includes(unavailable === 'busy' ? copy.sourceChangesBusy : copy.sourceChangesEditing))
    for (const label of [copy.sourceChangesRegenerate, copy.sourceChangesIgnore]) assert.ok(notice.button(label)!.props.disabled)
    notice.unmount()
  })
}

test('read-only users see source changes and no mutation controls', () => {
  const notice = mountNotice({ canEdit: false })
  assert.ok(visibleText(notice.root).includes(zhCN.knowledgeEditor.wikiBrowser.sourceChangesTitle))
  assert.equal(descendants(notice.root).filter(node => node.tag === 'button').length, 0)
  notice.unmount()
})

for (const source_changes of [{}, undefined, null]) {
  test(`pages with source_changes ${JSON.stringify(source_changes)} render no marker`, () => {
    const notice = mountNotice({ initialPage: page({ source_changes: source_changes as any }) })
    assert.equal(visibleText(notice.root), '')
    assert.equal(notice.requests.length, 0)
    notice.unmount()
  })
}

test('the API passes hierarchical page slugs in JSON and uses the agreed source change endpoints', () => {
  const calls: unknown[][] = []
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('../../../api/wiki/index.ts', import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  new Function('require', 'module', 'exports', code)(() => ({ post: (...args: unknown[]) => calls.push(args) }), module, module.exports)
  module.exports.ignoreWikiSourceChanges('kb-a', 'concept/来源 页面')
  module.exports.regenerateWikiSourceChanges('kb-a', 'concept/来源 页面')
  assert.deepEqual(calls, [
    ['/api/v1/knowledgebase/kb-a/wiki/source-changes/ignore', { slug: 'concept/来源 页面' }],
    ['/api/v1/knowledgebase/kb-a/wiki/source-changes/regenerate', { slug: 'concept/来源 页面' }],
  ])
})
