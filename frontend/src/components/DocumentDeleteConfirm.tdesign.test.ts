import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString, type SSRContext } from '@vue/server-renderer'
import { createSSRApp, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'

const require = createRequire(import.meta.url)
const { Alert, Button, Dialog, Icon } = require('tdesign-vue-next') as typeof import('tdesign-vue-next')
const messages = { 'zh-CN': zh, 'en-US': en }
const escapeHTML = (value: string) => value.replace(/&/g, '&amp;').replace(/"/g, '&quot;')
  .replace(/'/g, '&#39;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const { descriptor } = parse(readFileSync(new URL('./DocumentDeleteConfirm.vue', import.meta.url), 'utf8'), {
  filename: 'DocumentDeleteConfirm.vue',
})
const script = compileScript(descriptor, { id: 'document-delete-portal' })
const template = compileTemplate({ id: 'document-delete-portal', filename: 'DocumentDeleteConfirm.vue',
  source: descriptor.template!.content, ssr: true, ssrCssVars: [],
  compilerOptions: { bindingMetadata: script.bindings },
})
assert.deepEqual(template.errors, [])

function evaluate(code: string, modules: Record<string, unknown> = {}) {
  const output = ts.transpileModule(code, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)(
    (name: string) => modules[name] ?? require(name), module, module.exports,
  )
  return module.exports
}

async function renderConfirmation(locale: keyof typeof messages, kind: 'mine' | 'article',
  action: (state: any) => void | Promise<void>, deleteAction: () => Promise<void>) {
  const i18n = createI18n({ legacy: false, locale, messages })
  const component = evaluate(script.content, { 'vue-i18n': { useI18n: () => i18n.global } }).default
  component.ssrRender = evaluate(template.code).ssrRender
  const originalSetup = component.setup
  let state: any
  component.setup = (props: object, context: object) => {
    state = originalSetup(props, context)
    onServerPrefetch(() => action(state))
    return state
  }
  const label = i18n.global.t(kind === 'mine' ? 'personalDocument.delete' : 'knowledgeBase.deleteDocument')
  const title = 'A complete document title for confirmation'
  const confirmation = i18n.global.t(kind === 'mine' ? 'personalDocument.deleteConfirm' : 'knowledgeBase.confirmDeleteDocument',
    { title, fileName: title })
  const deleted: string[] = [], errors: unknown[] = [], warnings: string[] = []
  let dialogProps: any, dialogInstance: any
  const app = createSSRApp(component, { documentId: `${kind}-document`, label, confirmation, hideTrigger: true,
    failureMessage: 'Deletion failed', deleteAction, onDeleted: (id: string) => deleted.push(id) })
  app.use(i18n)
  // Observe the actual Dialog props without replacing its implementation.
  app.mixin({ created() { if (this.$options.name === Dialog.name) { dialogProps = this.$props; dialogInstance = this.$ } } })
  app.component('t-dialog', Dialog)
  app.component('t-button', Button)
  app.component('t-alert', Alert)
  app.component('t-icon', Icon)
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const context: SSRContext = {}
  const primaryHTML = await renderToString(app, context)
  // The unresolved client teleport target makes Vue SSR skip the portal
  // subtree. Serialize the actual modal vnode produced by the library,
  // preserving the real DialogCard, Button and Alert.
  // This verifies library markup without claiming a mounted browser portal.
  const childNodes = (vnode: any) => Array.isArray(vnode.children) ? vnode.children : vnode.children.default()
  const modalApp = createSSRApp({ render: () => childNodes(childNodes(dialogInstance.subTree)[0])[0] })
  modalApp.use(i18n)
  const html = await renderToString(modalApp)
  assert.deepEqual(errors, [], 'The actual TDesign components render without exceptions')
  assert.deepEqual(warnings, [], 'The real template resolves all installed components')
  assert.equal(dialogProps.attach, 'body')
  assert.equal(dialogProps.mode, 'modal')
  assert.equal(dialogProps.showOverlay, true)
  assert.equal(dialogProps.visible, true)
  assert.match(html, /t-dialog__ctx--fixed/)
  assert.match(html, /t-dialog__mask/)
  assert.ok(html.includes(label))
  assert.ok(html.includes(escapeHTML(confirmation)))
  // Installed TDesign resolves its teleport target in onMounted. SSR does not
  // invoke that hook, and the primary render has no mounted portal content.
  assert.doesNotMatch(primaryHTML, /t-dialog__ctx/)
  assert.equal(context.teleports?.body || '', '')
  return { html, dialogProps, deleted, state, label, confirmation }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const kind of ['mine', 'article'] as const) {
    test(`${locale} ${kind}: installed TDesign receives body modal props and renders the complete confirmation`, async () => {
      let calls = 0
      const result = await renderConfirmation(locale, kind, state => state.open(), async () => { calls++ })
      assert.equal(calls, 0)
      assert.equal(result.dialogProps.closeBtn, true)
      assert.equal(result.dialogProps.closeOnOverlayClick, true)
      assert.equal(result.dialogProps.closeOnEscKeydown, true)
      assert.match(result.html, /t-dialog__close/)
      assert.match(result.html, /t-dialog__confirm/)
      assert.match(result.html, /t-dialog__cancel/)
    })

    test(`${locale} ${kind}: installed TDesign hides closing controls and disables both actions while DELETE is pending`, async () => {
      let calls = 0, release: () => void = () => {}, pending: Promise<void> | undefined
      const result = await renderConfirmation(locale, kind, async state => {
        state.open()
        pending = state.confirm()
        await state.confirm()
        state.close()
        assert.equal(state.visible.value, true)
      }, () => { calls++; return new Promise(resolve => { release = resolve }) })
      assert.equal(calls, 1)
      assert.equal(result.dialogProps.closeBtn, false)
      assert.equal(result.dialogProps.closeOnOverlayClick, false)
      assert.equal(result.dialogProps.closeOnEscKeydown, false)
      assert.equal(result.dialogProps.confirmBtn.disabled, true)
      assert.equal(result.dialogProps.cancelBtn.disabled, true)
      assert.doesNotMatch(result.html, /class="t-dialog__close/)
      assert.match(result.html, /<button[^>]*class="[^"]*t-dialog__confirm[^"]*"[^>]*disabled/)
      assert.match(result.html, /<button[^>]*class="[^"]*t-dialog__cancel[^"]*"[^>]*disabled/)
      release()
      await pending
      assert.equal(result.state.visible.value, false)
      assert.deepEqual(result.deleted, [`${kind}-document`])
    })

    test(`${locale} ${kind}: installed TDesign keeps inline failure visible with enabled retry after a failed DELETE`, async () => {
      let calls = 0
      const result = await renderConfirmation(locale, kind, async state => { state.open(); await state.confirm() }, async () => {
        if (++calls === 1) throw new Error('Fixture storage failure with retry context')
      })
      assert.equal(calls, 1)
      assert.match(result.html, /role="alert"/)
      assert.ok(result.html.includes('Fixture storage failure with retry context'))
      assert.equal(result.dialogProps.confirmBtn.disabled, false)
      assert.equal(result.dialogProps.cancelBtn.disabled, false)
      assert.equal(result.dialogProps.closeBtn, true)
      await result.state.confirm()
      assert.equal(calls, 2)
      assert.equal(result.state.visible.value, false)
      assert.deepEqual(result.deleted, [`${kind}-document`])
    })
  }
}
