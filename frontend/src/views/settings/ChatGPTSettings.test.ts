import assert from 'node:assert/strict'
import { readFileSync, appendFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { click, descendants, element, hasClass, registerPrimitives, renderer, settle, visibleText } from '../knowledge/componentTestHarness'

const require = createRequire(import.meta.url)
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
const messages = { 'zh-CN': zh, 'en-US': en }
const account = { id: 'account-a', email: 'user@example.com', subject: 'subject-a', client_id: 'oaiapp_workspace_a', status: 'connected', expires_at: '2026-10-11T10:00:00Z', scopes: [], welcome_acknowledged: true }
const initialPolicy = { daily_limit: 5, daily_used: 5, day: '2026-10-11', paused: true, reason: 'subscription_sharing_usage_limit_exceeded', fallback_model_id: '' }

function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
  return module.exports
}
function component(url: URL, modules: Record<string, unknown>) {
  const { descriptor } = parse(readFileSync(url, 'utf8'), { filename: url.pathname })
  const script = compileScript(descriptor, { id: url.pathname })
  const template = compileTemplate({ id: url.pathname, filename: url.pathname, source: descriptor.template!.content, compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const result = evaluate(script.content, modules).default
  result.render = evaluate(template.code, modules).render
  let state: any
  const setup = result.setup
  result.setup = (props: object, context: object) => { state = setup(props, context); return state }
  return { result, state: () => state }
}
async function mount(locale: 'zh-CN' | 'en-US', options: { encryption?: boolean; accounts?: any[]; failedLoad?: boolean; failedResume?: boolean; logoutUnconfirmed?: boolean; blankPolicy?: boolean; unlimited?: boolean } = {}) {
  const calls: { path: string; body?: any }[] = []
  let accounts = options.accounts || [structuredClone(account)], policy = { ...(options.blankPolicy ? { ...initialPolicy, daily_used: 0, paused: false } : structuredClone(initialPolicy)), daily_limit: options.unlimited ? null : initialPolicy.daily_limit }
  const request = {
    get: async (path: string) => {
      calls.push({ path })
      if (options.failedLoad) throw new Error('fixture load failure')
      if (path.endsWith('/accounts')) return { success: true, data: { accounts: structuredClone(accounts), encryption_ready: options.encryption !== false, callback_url: 'http://127.0.0.1:8080/auth/chatgpt/callback' } }
      assert.equal(path, '/api/v1/chatgpt/policy'); return { success: true, data: structuredClone(policy) }
    },
    put: async (path: string, body: any) => { calls.push({ path, body }); policy = { ...policy, ...body }; return { success: true, data: structuredClone(policy) } },
    post: async (path: string, body: any) => {
      calls.push({ path, body })
      if (path.endsWith('/resume')) { if (options.failedResume) throw new Error('fixture resume failure'); policy.paused = false; return { success: true, data: structuredClone(policy) } }
      if (path.endsWith('/login')) return { success: true, data: { authorization_url: 'http://127.0.0.1:8080/auth/chatgpt/continue?authorization_attempt=opaque', expires_at: new Date(Date.now() + 600000).toISOString() } }
      if (path.endsWith('/logout')) { accounts = accounts.map(value => ({ ...value, status: 'signed_out', ...(options.logoutUnconfirmed ? { last_error: 'revocation_unconfirmed' } : {}) })); return { success: true, data: { remote_revocation_confirmed: !options.logoutUnconfirmed } } }
      if (path.endsWith('/ack')) accounts = accounts.map(value => ({ ...value, welcome_acknowledged: true }))
      return { success: true, data: {} }
    },
  }
  const modules: Record<string, unknown> = {
    '@/utils/request': request, '@/api/model': {},
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
    '@/assets/img/chatgpt-logo-white.svg': moduleOf('official-logo.svg'),
  }
  modules['@/api/chatgpt'] = evaluate(readFileSync(new URL('../../api/chatgpt.ts', import.meta.url), 'utf8'), modules)
  modules['@/components/ChatGPTUsageStatus.vue'] = moduleOf(component(new URL('../../components/ChatGPTUsageStatus.vue', import.meta.url), modules).result)
  const view = component(new URL('./ChatGPTSettings.vue', import.meta.url), modules)
  const app = renderer.createApp(view.result, { models: [
    { id: 'fallback', name: 'qwen3', type: 'KnowledgeQA', source: 'local', status: 'active', parameters: {} },
    { id: 'chatgpt', name: 'chatgpt-slug', type: 'KnowledgeQA', source: 'chatgpt', parameters: {} },
    { id: 'embedding', name: 'embed', type: 'Embedding', source: 'remote', parameters: {} },
  ] })
  app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
  delete app._context.components['t-dialog']
  app.component('t-dialog', defineComponent({ props: ['visible', 'header'], setup: (props, { slots }) => () => props.visible ? h('section', { role: 'dialog' }, [props.header, slots.default?.()]) : null }))
  app.component('t-input-number', defineComponent({ props: ['modelValue'], setup: (props, { attrs }) => () => h('input', { ...attrs, value: props.modelValue ?? '' }) }))
  const root = element('root'), errors: unknown[] = [], warnings: string[] = []
  app.config.errorHandler = err => errors.push(err); app.config.warnHandler = warning => warnings.push(warning)
  app.mount(root); await settle()
  return { app, root, state: view.state(), calls, errors, warnings, setAccounts: (value: any[]) => { accounts = value } }
}
test('English unlimited daily limit loads as empty input with a full placeholder and sufficient responsive width', async () => {
  const view = await mount('en-US', { unlimited: true })
  try {
    assert.deepEqual(view.calls.map(call => call.path), ['/api/v1/chatgpt/accounts', '/api/v1/chatgpt/policy'])
    const input = descendants(view.root).find(node => node.tag === 'input' && node.props.id === 'chatgpt-daily-limit')!
    assert.ok(input)
    assert.equal(input.props.value, '')
    assert.equal(input.props.placeholder, 'Unlimited')
    assert.ok(hasClass(input, 'chatgpt-daily-limit'))
    const { descriptor } = parse(readFileSync(new URL('./ChatGPTSettings.vue', import.meta.url), 'utf8'))
    const width = descriptor.styles.map(style => style.content).join('\n').match(/\.chatgpt-daily-limit\s*\{([^}]+)\}/)?.[1]
    assert.ok(width)
    assert.match(width, /\bwidth:\s*200px;/)
    assert.match(width, /\bmax-width:\s*100%;/)
    assert.deepEqual(view.errors, [])
    assert.deepEqual(view.warnings, [])
  } finally { view.app.unmount() }
})
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: ChatGPT settings load accounts and quota through the real API and save limits, fallback and manual resume`, async () => {
    const view = await mount(locale)
    try {
      const copy = messages[locale].chatgpt
      assert.ok(visibleText(view.root).includes('Continue with ChatGPT'))
      assert.ok(visibleText(view.root).includes(copy.pausedTitle))
      assert.ok(visibleText(view.root).includes(copy.pauseReasons.exceeded))
      assert.ok(visibleText(view.root).includes('user@example.com (kspace_a)'))
      assert.deepEqual(view.calls.map(call => call.path), ['/api/v1/chatgpt/accounts', '/api/v1/chatgpt/policy'])
      assert.deepEqual(view.state.fallbackModels.value.map((model: any) => model.id), ['fallback'])
      view.state.dailyLimit.value = undefined; view.state.fallbackID.value = 'fallback'; await view.state.save(); await settle()
      assert.deepEqual(view.calls.at(-1), { path: '/api/v1/chatgpt/policy', body: { daily_limit: null, fallback_model_id: 'fallback' } })
      const manage = descendants(view.root).find(node => node.props.href === 'https://chatgpt.com/settings/usage')!
      assert.ok(manage); assert.equal(manage.props.rel, 'noopener noreferrer')
      const resume = descendants(view.root).find(node => node.tag === 'button' && visibleText(node) === copy.resume)!
      assert.ok(resume); await click(resume)
      assert.equal(view.state.policy.value.paused, false)
      assert.equal(view.calls.at(-1)?.path, '/api/v1/chatgpt/resume')
      assert.deepEqual(view.errors, []); assert.deepEqual(view.warnings, [])
    } finally { view.app.unmount() }
  })
  test(`${locale}: missing encryption blocks sign-in and account refresh failure offers retry`, async () => {
    const view = await mount(locale, { encryption: false })
    try {
      const button = descendants(view.root).find(node => hasClass(node, 'chatgpt-signin'))!
      assert.equal(button.props.disabled, true)
      assert.ok(visibleText(view.root).includes(messages[locale].chatgpt.encryptionRequired))
    } finally { view.app.unmount() }
    const failed = await mount(locale, { failedLoad: true })
    try { assert.ok(visibleText(failed.root).includes('fixture load failure')); assert.equal(failed.state.loaded.value, false) }
    finally { failed.app.unmount() }
  })
  test(`${locale}: refresh failure and invalid daily limits retain paused status and entered values`, async () => {
    const view = await mount(locale, { failedResume: true })
    try {
      view.state.dailyLimit.value = 0; await view.state.save(); await settle()
      assert.ok(visibleText(view.root).includes(messages[locale].chatgpt.invalidLimit))
      assert.equal(view.calls.filter(call => call.body).length, 0)
      await view.state.resume(); await settle()
      assert.equal(view.state.policy.value.paused, true)
      assert.ok(visibleText(view.root).includes('fixture resume failure'))
    } finally { view.app.unmount() }
  })
}
test('ChatGPT login opens a new tab, polls validated accounts and persists the first-use welcome acknowledgement', async () => {
  const previous = globalThis.window, opened: string[] = []
  globalThis.window = { open: () => ({ opener: {}, location: { replace: (url: string) => opened.push(url) }, close() {} }) } as any
  const view = await mount('en-US', { accounts: [] })
  try {
    await click(descendants(view.root).find(node => hasClass(node, 'chatgpt-signin'))!)
    assert.equal(view.calls.at(-1)?.path, '/api/v1/chatgpt/login')
    assert.match(opened[0], /^http:\/\/127\.0\.0\.1:8080\/auth\/chatgpt\/continue\?/)
    view.setAccounts([{ ...account, welcome_acknowledged: false }]); await view.state.pollLogin(Date.now() + 10000); await settle()
    assert.equal(view.state.loginPending.value, false)
    assert.ok(visibleText(view.root).includes(en.chatgpt.welcomeTitle))
    await view.state.acknowledgeWelcome(); await settle()
    assert.deepEqual(view.calls.at(-1), { path: '/api/v1/chatgpt/welcome/ack', body: { account_id: account.id } })
    assert.equal(view.state.welcomeAccount.value, null)
    await view.state.load(); assert.equal(view.state.welcomeAccount.value, null)
    await view.state.signOut(account.id); assert.equal(view.state.accounts.value[0].status, 'signed_out')
  } finally { view.app.unmount(); globalThis.window = previous }
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: runtime queue loads the ChatGPT paused and reauthentication state through the actual API and resumes`, async () => {
    const calls: string[] = [], policy = { ...initialPolicy, reason: 'subscription_sharing_usage_unavailable', retry_at: '2026-10-11T10:05:00Z' }
    const modules: Record<string, unknown> = {
      '@/utils/request': { get: async (path: string) => {
        if (path === '/health') return { status: 'ok', queue_connection: { state: 'active', alert: false, alert_after_seconds: 300 } }
        calls.push(path)
        if (path.endsWith('/accounts')) return { success: true, data: { accounts: [{ ...account, status: 'needs_login' }], encryption_ready: true, callback_url: '' } }
        if (path.endsWith('/policy')) return { success: true, data: { ...policy } }
        assert.equal(path, '/api/v1/system/runtime/queues'); return { available: true, queues: [], pools: [], models: [] }
      }, post: async (path: string) => { calls.push(path); policy.paused = false; return { success: true, data: { ...policy } } } },
      'tdesign-vue-next': { MessagePlugin: {} },
      '@/i18n/countCopy': require('../../i18n/countCopy'),
      './runtimeTaskPagination': require('../system/runtimeTaskPagination'),
      '@/components/settings/SettingDrawer.vue': moduleOf(defineComponent({ setup: () => () => null })),
    }
    modules['@/api/chatgpt'] = evaluate(readFileSync(new URL('../../api/chatgpt.ts', import.meta.url), 'utf8'), modules)
    modules['@/api/system'] = evaluate(readFileSync(new URL('../../api/system/index.ts', import.meta.url), 'utf8'), modules)
    modules['@/components/ChatGPTUsageStatus.vue'] = moduleOf(component(new URL('../../components/ChatGPTUsageStatus.vue', import.meta.url), modules).result)
    modules['./QueueConnectionNotice.vue'] = moduleOf(component(new URL('../system/QueueConnectionNotice.vue', import.meta.url), modules).result)
    const view = component(new URL('../system/RuntimeQueues.vue', import.meta.url), modules)
    const app = renderer.createApp(view.result)
    app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
    for (const name of ['t-switch', 't-skeleton', 't-progress', 't-empty', 't-table']) app.component(name, defineComponent({ setup: () => () => null }))
    const root = element('root'), errors: unknown[] = [], warnings: string[] = []
    app.config.errorHandler = err => errors.push(err); app.config.warnHandler = warning => warnings.push(warning)
    try {
      app.mount(root); await settle()
      assert.ok(visibleText(root).includes(messages[locale].chatgpt.pauseReasons.unavailable))
      assert.ok(visibleText(root).includes(messages[locale].chatgpt.reloginAccount.replace('{account}', 'user@example.com (kspace_a)')))
      assert.ok(calls.includes('/api/v1/chatgpt/accounts')); assert.ok(calls.includes('/api/v1/chatgpt/policy')); assert.ok(calls.includes('/api/v1/system/runtime/queues'))
      const resume = descendants(root).find(node => node.tag === 'button' && visibleText(node) === messages[locale].chatgpt.resume)!
      await click(resume); assert.equal(view.state().chatgptPolicy.value.paused, false)
      assert.equal(calls.at(-1), '/api/v1/chatgpt/resume')
      assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
    } finally { app.unmount() }
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: logout warns when remote revocation was not confirmed, including after reloading accounts`, async () => {
    const view = await mount(locale, { logoutUnconfirmed: true })
    try {
      await view.state.signOut(account.id); await settle()
      assert.equal(view.state.accounts.value[0].status, 'signed_out')
      assert.ok(visibleText(view.root).includes(messages[locale].chatgpt.revocationUnconfirmed))
      await view.state.load(); await settle()
      assert.ok(visibleText(view.root).includes(messages[locale].chatgpt.revocationUnconfirmed))
    } finally { view.app.unmount() }
  })
  test(`${locale}: a local-only installation does not claim to be using a ChatGPT plan`, async () => {
    const view = await mount(locale, { accounts: [], blankPolicy: true })
    try { assert.equal(descendants(view.root).some(node => hasClass(node, 'chatgpt-usage')), false) }
    finally { view.app.unmount() }
  })
}

test('migration account relogin page submits only the existing account identity',async()=>{
 const previous=globalThis.window
 globalThis.window={open:()=>({opener:{},location:{replace(){}},close(){}})} as any
 const view=await mount('zh-CN',{accounts:[{...account,status:'needs_login'}]})
 try {
  await view.state.signIn(account.id)
  const request=view.calls.find(x=>x.path==='/api/v1/chatgpt/login')!
  assert.deepEqual(request.body,{account_id:account.id})
  if(process.env.RELIQUARY_RECOVERY_REQUESTS)appendFileSync(process.env.RELIQUARY_RECOVERY_REQUESTS,JSON.stringify({kind:'chatgpt_login',method:'POST',body:request.body})+'\n')
 }finally{view.app.unmount();globalThis.window=previous}
})
