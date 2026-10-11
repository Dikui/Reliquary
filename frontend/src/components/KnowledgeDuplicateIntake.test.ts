import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc';
import { renderToString } from '@vue/server-renderer';
import * as vue from 'vue';
import { createI18n } from 'vue-i18n';
import ts from 'typescript';
import zhCN from '../i18n/locales/zh-CN';
import enUS from '../i18n/locales/en-US';
import * as duplicate from '../utils/knowledgeDuplicate';
import * as knowledgeStatus from '../views/knowledge/knowledgeStatusPolling';

const require = createRequire(import.meta.url);
const installedAlert = require('tdesign-vue-next').Alert;
const componentFiles = {
  notice: new URL('./KnowledgeDuplicateNotice.vue', import.meta.url),
  manual: new URL('./ManualKnowledgeEditor.vue', import.meta.url),
  knowledgeBase: new URL('../views/knowledge/KnowledgeBase.vue', import.meta.url),
};

function compile(code: string) {
  return ts.transpileModule(code, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
}

function createEnvironment(locale = 'zh-CN') {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } });
  const t = i18n.global.t.bind(i18n.global);
  const events: CustomEvent[] = [];
  const pushes: any[] = [];
  const toasts: Array<{ kind: string; text: string }> = [];
  const renderedButtons: any[] = [];
  const requests: Array<{ kind: string; payload: any }> = [];
  const originalWindow = globalThis.window;
  globalThis.window = { innerHeight: 900, dispatchEvent: (event: CustomEvent) => { events.push(event); return true; } } as unknown as Window & typeof globalThis;
  const route = { name: 'knowledge', params: {}, query: {} };
  const uiStore = {
    manualEditorVisible: true, manualEditorMode: 'create', manualEditorKnowledgeId: null,
    manualEditorKBId: 'kb-a', manualEditorInitialTitle: 'New source',
    manualEditorInitialContent: 'The collected source content remains in this draft.',
    manualEditorInitialStatus: 'draft',
    closeManualEditor() { this.manualEditorVisible = false; },
    notifyManualEditorSuccess() {},
  };
  const state = { response: { success: true, data: { id: 'new-message' } } as any, reject: false };
  async function intake(kind: string, _kbId: string, payload: any) {
    requests.push({ kind, payload });
    if (state.reject) throw state.response;
    return state.response;
  }
  const envelope = (data: any) => Promise.resolve({ success: true, data });
  const api: any = {
    createManualKnowledge: (kbId: string, payload: any) => intake('manual', kbId, payload),
    createKnowledgeFromURL: (kbId: string, payload: any) => intake('url', kbId, payload),
    uploadKnowledgeFile: (kbId: string, payload: any) => intake('file', kbId, payload),
    getKnowledgeBase: () => envelope({ id: 'kb-a' }),
    getKnowledgeDetails: () => envelope({ id: 'owner-a', knowledge_base_id: 'kb-a', title: 'Existing article' }),
    getKnowledgeDetailsCon: () => Promise.resolve({ success: true, data: [], total: 0 }),
    listKnowledgeFiles: () => Promise.resolve({ success: true, data: [], total: 0 }),
  };
  const emptyComponent = vue.defineComponent({ setup: () => () => null });
  const store = { cardList: vue.ref<any[]>([]), total: vue.ref(0) };
  const modules: Record<string, any> = {
    vue: { ...vue, watch: () => () => {}, onMounted() {}, onUnmounted() {}, onBeforeUnmount() {} },
    pinia: { storeToRefs: (value: any) => value },
    'vue-router': { useRoute: () => route, useRouter: () => ({ push: async (value: any) => { pushes.push(value); } }) },
    'vue-i18n': { useI18n: () => ({ t }) },
    'tdesign-vue-next': { MessagePlugin: Object.fromEntries(['success', 'info', 'warning', 'error'].map(kind => [kind, (text: string) => toasts.push({ kind, text })])) },
    marked: { marked: { parse: (text: string) => text, use() {} } },
    '@/api/knowledge-base': api,
    '@/stores/knowledge': { knowledgeStore: () => store },
    '@/stores/ui': { useUIStore: () => uiStore },
    '@/stores/menu': { useMenuStore: () => ({}) },
    '@/stores/auth': { useAuthStore: () => ({ isLoggedIn: true, effectiveTenantId: '' }) },
    '@/stores/commandPalette': { useCommandPaletteStore: () => ({}) },
    '@/stores/resources': { useResourcesStore: () => ({ knowledgeBase: { id: 'kb-a' } }) },
    '@/stores/editorResources': { useEditorResourcesStore: () => ({ parserEngines: [] }) },
    '@/stores/uploadConfirm': { useUploadConfirmStore: () => ({ open: async () => ({ manual: { sourceInfo: { publisher: 'Publisher A' } } }) }) },
    '@/utils/security': { sanitizeHTML: (value: string) => value, safeMarkdownToHTML: (value: string) => value },
    '@/types/knowledgeClassification': { CONTENT_TYPES: ['event', 'opinion', 'research', 'tutorial', 'case', 'reference'] },
    '@/utils/knowledgeDuplicate': duplicate,
    '../utils/knowledgeDuplicate': duplicate,
    '../utils/index': { formatStringDate: (value: Date) => value.toISOString(), kbFileTypeVerification: () => false },
    './knowledgeStatusPolling': knowledgeStatus,
    './knowledgeDownloadFileName': {},
    '@/utils/knowledgeTrace': {},
    './utils/uploadSources': { getUploadFileKey: (file: File) => `${file.name}\0${file.size}` },
    '@/api/user-favorites': {},
    '@/composables/useMarqueeSelect': { useMarqueeSelect: () => ({ marqueeVisible: vue.ref(false), marqueeMode: vue.ref('select'), boxStyle: vue.ref({}), shouldSuppressClick: () => false }) },
  };
  function evaluate(code: string) {
    const module = { exports: {} as any };
    new Function('require', 'module', 'exports', compile(code))((name: string) => {
      if (name in modules) return modules[name];
      if (name.endsWith('.vue')) return { __esModule: true, default: emptyComponent };
      return require(name);
    }, module, module.exports);
    return module.exports;
  }
  modules['./useKnowledgeDuplicates'] = modules['@/composables/useKnowledgeDuplicates'] = evaluate(readFileSync(new URL('../composables/useKnowledgeDuplicates.ts', import.meta.url), 'utf8'));
  const knowledgeHook = evaluate(readFileSync(new URL('../composables/useKnowledgeBase.ts', import.meta.url), 'utf8')).default;
  modules['@/composables/useKnowledgeBase'] = { __esModule: true, default: knowledgeHook };

  function loadSFC(kind: keyof typeof componentFiles) {
    const filename = componentFiles[kind].pathname;
    const { descriptor } = parse(readFileSync(componentFiles[kind], 'utf8'), { filename });
    const script = compileScript(descriptor, { id: `duplicate-${kind}-test` });
    const template = compileTemplate({ id: `duplicate-${kind}-test`, filename, source: descriptor.template!.content,
      ssr: kind !== 'notice', ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } });
    assert.deepEqual(template.errors, []);
    const component = evaluate(script.content).default;
    Object.assign(component, evaluate(template.code));
    return component;
  }
  const notice = loadSFC('notice');
  modules['@/components/KnowledgeDuplicateNotice.vue'] = { __esModule: true, default: notice };
  const button = vue.defineComponent({ setup: (_props, { attrs, slots }) => () => {
    const node = vue.h('button', attrs, slots.default?.());
    renderedButtons.push(node);
    return node;
  } });
  const alert = vue.defineComponent({ props: ['message'], setup: (props, { attrs, slots }) => () => vue.h('div', attrs, [slots.default?.() || props.message, slots.operation?.()]) });
  const generic = vue.defineComponent({ setup: (_props, { attrs, slots }) => () => vue.h('div', attrs, [slots.default?.(), slots['footer-right']?.()]) });
  modules['@/components/settings/SettingDrawer.vue'] = { __esModule: true, default: generic };

  async function render(kind: 'manual' | 'knowledgeBase', scenario: (value: any) => Promise<void>, actualAlert = false) {
    const component = loadSFC(kind);
    const setup = component.setup;
    let captured: any;
    component.setup = (props: any, context: any) => {
      captured = setup(props, context);
      vue.onServerPrefetch(async () => { await scenario(captured); await vue.nextTick(); });
      return captured;
    };
    const app = vue.createSSRApp(component);
    app.config.globalProperties.$t = t;
    for (const name of ['t-input', 't-textarea', 't-icon', 't-tag', 't-select', 't-switch', 't-tooltip', 't-dropdown', 't-date-range-picker', 't-skeleton', 't-loading', 't-input-number', 't-popup']) app.component(name, generic);
    app.component('t-button', button);
    app.component('t-alert', actualAlert ? installedAlert : alert);
    const errors: unknown[] = [];
    app.config.errorHandler = error => { errors.push(error); };
    let html: string;
    try { html = await renderToString(app); }
    catch (error) { assert.deepEqual(errors, [], 'the intake SFC setup completes'); throw error; }
    assert.deepEqual(errors, [], 'the actual intake SFC and duplicate notice render without exceptions');
    return { html, value: captured };
  }

  function textOf(node: any): string {
    if (typeof node === 'string') return node;
    if (Array.isArray(node)) return node.map(textOf).join('');
    return node?.children ? textOf(node.children) : '';
  }
  function action(key: string) {
    const target = renderedButtons.find(node => textOf(node) === t(key as any));
    assert.ok(target, `the rendered ${key} button is present`);
    assert.equal(target.type, 'button');
    assert.equal(typeof target.props.onClick, 'function');
    return target.props.onClick();
  }
  return { api, render, action, t, route, state, uiStore, toasts, events, pushes, requests, knowledgeHook,
    cleanup: () => { globalThis.window = originalWindow; } };
}

function duplicateResponse(code = 'duplicate_manual', method?: string) {
  return { status: 409, success: false, code, message: 'Message already exists', match_method: method,
    data: { id: 'owner-a', knowledge_base_id: 'kb-a', title: 'Existing article', repost_sources: [{ match_method: 'simhash' }] } };
}

for (const locale of ['zh-CN', 'en-US']) {
  for (const method of [undefined, 'content_hash', 'simhash']) {
    test(`${locale} manual duplicate ${method || 'legacy'} retains the draft and opens the existing article`, async () => {
      const env = createEnvironment(locale);
      try {
        env.state.response = duplicateResponse('duplicate_manual', method);
        env.state.reject = true;
        const { html, value } = await env.render('manual', async value => { await value.initialize(); await value.handleSave('publish'); });
        assert.match(html, /knowledge-duplicate-notice/);
        assert.ok(html.includes(env.t(method === 'simhash' ? 'knowledgeBase.duplicateSimilarFound' : 'knowledgeBase.duplicateFound')));
        assert.ok(html.includes(env.t('knowledgeBase.viewExistingMessage')));
        assert.doesNotMatch(html, /Message already exists/);
        assert.equal(value.form.content, env.uiStore.manualEditorInitialContent);
        assert.equal(value.form.title, 'New source');
        assert.equal(value.saving.value, false);
        assert.equal(env.uiStore.manualEditorVisible, true);
        assert.equal(value.currentKnowledgeId.value, null);
        assert.deepEqual(env.toasts, []);
        assert.equal(env.requests[0].payload.source_info.publisher, 'Publisher A');
        await env.action('knowledgeBase.viewExistingMessage');
        assert.equal(env.uiStore.manualEditorVisible, false);
        assert.deepEqual(env.events[0].detail, { kbId: 'kb-a', knowledgeId: 'owner-a' });
        assert.deepEqual(env.pushes, []);
      } finally { env.cleanup(); }
    });
  }
}

for (const kind of ['file', 'url'] as const) {
  for (const rejected of [false, true]) {
    test(`${kind} duplicate ${rejected ? 'rejection' : 'response'} renders a localized working view action`, async () => {
      const env = createEnvironment();
      try {
        env.state.response = duplicateResponse(`duplicate_${kind}`, kind === 'file' ? 'file_hash' : 'url');
        env.state.reject = rejected;
        const { html, value } = await env.render('knowledgeBase', async value => {
          if (kind === 'file') {
            const result = await value.executeUploadBatch([new File(['same content'], 'same.md')]);
            assert.deepEqual(result, { successCount: 0, failCount: 0, duplicateCount: 1 });
          } else await value.executeUrlImport('https://example.invalid/source');
        });
        assert.ok(html.includes(env.t('knowledgeBase.duplicateFound')));
        assert.doesNotMatch(html, /Message already exists/);
        assert.deepEqual(env.toasts, [], 'a duplicate is not announced as an upload failure or success');
        await env.action('knowledgeBase.viewExistingMessage');
        assert.deepEqual(env.events[0].detail, { kbId: 'kb-a', knowledgeId: 'owner-a' });
        await env.action('knowledgeBase.dismissDuplicateNotice');
        assert.equal(value.duplicateNotices.value.length, 0);
      } finally { env.cleanup(); }
    });
  }
}

test('batch upload keeps duplicate feedback separate from real upload errors and successes', async () => {
  const env = createEnvironment();
  try {
    const responses = [duplicateResponse('duplicate_file', 'content_hash'), { success: true, data: { id: 'new' } }, { success: false, message: 'Upload rejected' }];
    env.api.uploadKnowledgeFile = async () => responses.shift();
    await env.render('knowledgeBase', async value => {
      const files = ['a.md', 'b.md', 'c.md'].map(name => new File(['body'], name));
      assert.deepEqual(await value.executeUploadBatch(files), { successCount: 1, failCount: 1, duplicateCount: 1 });
      assert.equal(value.duplicateNotices.value.length, 1);
    });
    assert.equal(env.toasts[0].kind, 'warning');
    assert.deepEqual(env.events.map(event => event.type), ['knowledgeFileUploaded']);
  } finally { env.cleanup(); }
});

test('view action opens the unique article page with the existing preview query', async () => {
  const env = createEnvironment();
  try {
    env.state.response = duplicateResponse();
    env.state.reject = true;
    env.route.name = 'settings';
    await env.render('manual', async value => { await value.initialize(); await value.handleSave('draft'); });
    await env.action('knowledgeBase.viewExistingMessage');
    assert.deepEqual(env.pushes, [{ path: '/platform/knowledge', query: { knowledge_id: 'owner-a' } }]);
    assert.deepEqual(env.events, []);
  } finally { env.cleanup(); }
});

test('detail hook carries repost sources, resets them on selection and ignores an older detail response', async () => {
  const env = createEnvironment();
  try {
    const pending = new Map<string, (value: any) => void>();
    env.api.getKnowledgeDetails = (id: string) => new Promise(resolve => pending.set(id, resolve));
    const hook = env.knowledgeHook('kb-a');
    const repost = { id: 'repost-a', knowledge_id: 'owner-a', publisher: 'Publisher B', match_method: 'url', published_at_missing: true };
    hook.getCardDetails({ id: 'owner-a' });
    pending.get('owner-a')!({ success: true, data: { id: 'owner-a', updated_at: '2026-10-08T00:00:00Z', repost_sources: [repost] } });
    await vue.nextTick();
    assert.deepEqual(hook.details.repost_sources, [repost]);
    hook.getCardDetails({ id: 'slow-a' });
    assert.deepEqual(hook.details.repost_sources, []);
    hook.getCardDetails({ id: 'owner-b' });
    pending.get('owner-b')!({ success: true, data: { id: 'owner-b', updated_at: '2026-10-08T00:00:00Z', repost_sources: null } });
    await vue.nextTick();
    pending.get('slow-a')!({ success: true, data: { id: 'slow-a', updated_at: '2026-10-08T00:00:00Z', repost_sources: [repost] } });
    await vue.nextTick();
    assert.equal(hook.details.id, 'owner-b');
    assert.deepEqual(hook.details.repost_sources, []);
  } finally { env.cleanup(); }
});

test('legacy upload hook uses the same duplicate notice and clears the file input', async () => {
  const env = createEnvironment();
  try {
    env.state.reject = true;
    env.state.response = duplicateResponse('duplicate_file');
    const hook = env.knowledgeHook('kb-a');
    const input = { value: { value: 'same.md' } };
    await hook.requestMethod(new File(['same content'], 'same.md'), input);
    assert.equal(input.value.value, '');
    assert.equal(hook.duplicateNotices.value[0].id, 'owner-a');
    assert.deepEqual(env.toasts, []);
  } finally { env.cleanup(); }
});

test('duplicate parser recognizes nested error responses without guessing the method from repost history', () => {
  const response = duplicateResponse();
  assert.equal(duplicate.resolveKnowledgeDuplicate({ response: { data: response } })?.matchMethod, undefined);
  assert.equal(duplicate.resolveKnowledgeDuplicate({ error: { code: 'duplicate_url' }, data: { id: 'owner-a' } }, 'kb-a')?.knowledgeBaseId, 'kb-a');
  assert.equal(duplicate.resolveKnowledgeDuplicate({ code: 'other_error', match_method: 'simhash' }), null);
});

test('the installed TDesign alert displays the duplicate message once with both working actions', async () => {
  const env = createEnvironment();
  try {
    env.state.response = duplicateResponse('duplicate_manual', 'simhash');
    env.state.reject = true;
    const { html } = await env.render('manual', async value => { await value.initialize(); await value.handleSave('draft'); }, true);
    assert.equal(html.split(env.t('knowledgeBase.duplicateSimilarFound')).length - 1, 1);
    assert.match(html, /role="status"/);
    assert.match(html, /aria-live="polite"/);
    await env.action('knowledgeBase.viewExistingMessage');
    assert.equal(env.events[0].detail.knowledgeId, 'owner-a');
    await env.action('knowledgeBase.dismissDuplicateNotice');
  } finally { env.cleanup(); }
});
