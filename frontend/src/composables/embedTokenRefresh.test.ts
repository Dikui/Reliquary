import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { ModuleKind, ScriptTarget, transpileModule } from 'typescript'
import { nextTick, reactive, ref, watch } from 'vue'

function loadComposable(file: string, dependencies: Record<string, unknown>) {
  const source = readFileSync(new URL(file, import.meta.url), 'utf8')
  const { outputText } = transpileModule(source.replaceAll('import.meta.env.DEV', 'false'), {
    compilerOptions: { module: ModuleKind.CommonJS, target: ScriptTarget.ES2022 },
  })
  const exports: any = {}
  new Function('require', 'exports', outputText)((name: string) => {
    assert.ok(name in dependencies, `missing dependency ${name}`)
    return dependencies[name]
  }, exports)
  return exports
}

test('host token refresh reaches chat requests without replacing the signed session', async (t) => {
  const oldWindow = Object.getOwnPropertyDescriptor(globalThis, 'window')
  const storage = new Map<string, string>()
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: {
      parent: {},
      localStorage: {
        getItem: (key: string) => storage.get(key) ?? null,
        setItem: (key: string, value: string) => storage.set(key, value),
      },
    },
  })
  t.after(() => {
    if (oldWindow) Object.defineProperty(globalThis, 'window', oldWindow)
    else Reflect.deleteProperty(globalThis, 'window')
  })
  let mounted: () => void = () => {}
  let provideToken: (token: string, channelId?: string) => void = () => {}
  let resolveConfig: (value: unknown) => void = () => {}
  let createdSessions = 0
  let streamHandler: any
  const requests: Array<{ type: string; token: string }> = []
  const i18n = { useI18n: () => ({ locale: ref('en-US'), t: (key: string) => key }) }
  const vue = { ref, reactive, watch, nextTick, onMounted: (fn: () => void) => { mounted = fn }, onUnmounted: () => {} }
  const embed = {
    onEmbedHostContext: () => () => {},
    onEmbedHostLocale: () => () => {},
    onEmbedHostToken: (fn: typeof provideToken) => { provideToken = fn; return () => {} },
    getOrCreateEmbedVisitorId: () => 'visitor',
    parseEmbedTokenFromLocation: () => '',
    postEmbedBootstrapRequest: () => {},
    postEmbedReady: () => {},
    isEmbedSessionToken: (token: string) => token.startsWith('ems_'),
    embedChatSessionStorageKey: (id: string) => `session:${id}`,
    getEmbedConfig: () => new Promise(resolve => { resolveConfig = resolve }),
    createEmbedSession: async () => {
      createdSessions++
      return { data: { id: 'conversation', sig: 'signature' } }
    },
    getEmbedMessageList: async (_id: string, token: string) => {
      requests.push({ type: 'history', token })
      return { data: [] }
    },
    stopEmbedSession: async (_id: string, token: string) => { requests.push({ type: 'stop', token }) },
    relayEmbedWebhookEvent: (_id: string, token: string, _sid: string, _sig: string, body: { type: string }) => {
      requests.push({ type: body.type, token })
    },
    postEmbedMessageSent: () => {},
    postEmbedMessageReceived: () => {},
  }
  const { useEmbedBridge } = loadComposable('./useEmbedBridge.ts', {
    vue, 'vue-router': { useRoute: () => ({ query: {} }) }, 'vue-i18n': i18n,
    '@/api/embed': embed,
    '@/i18n/embed': { readEmbedLocaleFromUrl: () => '', applyEmbedLocale: () => {}, syncEmbedLocaleFromUrl: () => {} },
  })
  const bridge = useEmbedBridge(ref('channel'))
  mounted()
  provideToken('ems_initial', 'channel')
  provideToken('ems_during_bootstrap', 'channel')
  resolveConfig({ success: true, data: { agent_id: 'agent' } })
  await new Promise(setImmediate)
  assert.equal(bridge.token.value, 'ems_during_bootstrap')
  assert.equal(bridge.sessionId.value, 'conversation')
  assert.equal(bridge.sessionSig.value, 'signature')

  const { useEmbedChatSession } = loadComposable('./useEmbedChatSession.ts', {
    vue, 'vue-i18n': i18n, '@/api/embed': embed,
    '@/api/chat/streame': {
      useStream: () => ({
        error: ref(''), onChunk: () => {}, stopStream: () => {},
        startStream: async (body: { embed_token: string }) => {
          requests.push({ type: 'stream', token: body.embed_token })
          streamHandler.currentAssistantMessageId.value = 'assistant'
          streamHandler.onReplyComplete('answer')
        },
      }),
    },
    '@/utils/embedToast': { embedToast: () => {} },
    '@/utils/embedContext': { buildQueryWithHostContext: (query: string) => query },
    '@/utils/embedFile': {},
    '@/composables/useStickyBottomOnResize': { useStickyBottomOnResize: () => {} },
    '@/composables/useChatStreamHandler': {
      useChatStreamHandler: (options: unknown) => {
        streamHandler = options
        return { handleMsgList: () => {}, prepareForNewOutgoingMessage: () => {}, markInFlightAssistantStopped: () => {} }
      },
    },
  })
  const chat = useEmbedChatSession({
    sessionId: bridge.sessionId, sessionSig: bridge.sessionSig, visitorId: bridge.visitorId,
    channelId: 'channel', token: bridge.token, agentId: 'agent', kbIds: [],
  })
  assert.deepEqual(requests.shift(), { type: 'history', token: 'ems_during_bootstrap' })
  provideToken('ems_refreshed', 'channel')
  provideToken('ems_foreign', 'other-channel')
  provideToken('publish-token', 'channel')
  assert.equal(bridge.token.value, 'ems_refreshed')
  await chat.sendMsg('hello')
  chat.handleStopGeneration()
  assert.deepEqual(requests, [
    { type: 'message_sent', token: 'ems_refreshed' },
    { type: 'stream', token: 'ems_refreshed' },
    { type: 'message_received', token: 'ems_refreshed' },
    { type: 'stop', token: 'ems_refreshed' },
  ])
  assert.equal(createdSessions, 1)
  assert.equal(bridge.sessionId.value, 'conversation')
  assert.equal(bridge.sessionSig.value, 'signature')
  assert.equal(chat.messagesList[0].content, 'hello')
})
