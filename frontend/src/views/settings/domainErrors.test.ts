import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US'
import zhCN from '../../i18n/locales/zh-CN'
import * as personalServerError from '../../utils/personalServerError'
import { domainErrorMessage, KNOWLEDGE_DOMAIN_REFERENCED_CODE } from './domainErrors'

test('referenced domain deletion shows the reason returned with HTTP 409', () => {
  const reason = '有 3 个知识库正在使用该领域'
  assert.equal(domainErrorMessage({ status: 409, message: reason, error: { message: reason } }, '操作失败，请重试'), reason)
  assert.equal(domainErrorMessage({ status: 409, error: { message: reason } }, '操作失败，请重试'), reason)
  assert.equal(domainErrorMessage(new Error(reason), '操作失败，请重试'), reason)

  const source = readFileSync(new URL('./DomainSettings.vue', import.meta.url), 'utf8')
  assert.match(source, /return domainErrorMessage\(cause, t\('knowledgeDomains.failed'\), count => t\('knowledgeDomains.deleteReferenced', \{ count \}, count\)\)/)
  assert.match(source, /deleteKnowledgeDomain\(domain.id\).*catch \(cause\) \{ error.value = errorMessage\(cause\)/)
  assert.match(source, /t-alert v-if="error" theme="error" :message="error" role="alert"/)
})

type RejectResponse = (cause: { response: { status: number; data: unknown }; config: { url: string } }) => Promise<never>

function requestRejectionHandler(translate: (key: string) => string = key => key): RejectResponse {
  let rejected: RejectResponse | undefined
  const axios = {
    create: () => ({
      interceptors: {
        request: { use() {} },
        response: { use(_fulfilled: unknown, handler: RejectResponse) { rejected = handler } },
      },
    }),
  }
  const modules: Record<string, unknown> = {
    axios,
    './index': { generateRandomString: () => 'domain-error-test', MAX_FILE_SIZE_MB: 50 },
    '@/i18n': { global: { t: translate } },
    './personalServerError': personalServerError,
    './api-base': { getApiBaseUrl: () => '/api/v1' },
  }
  const source = readFileSync(new URL('../../utils/request.ts', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
  }).outputText
  // Execute the real request module with isolated dependencies and no network.
  new Function('require', 'exports', compiled)((name: string) => {
    assert.ok(Object.hasOwn(modules, name), `unexpected request module dependency: ${name}`)
    return modules[name]
  }, {})
  assert.ok(rejected)
  return rejected
}

async function rejectedDomainResponse(count: unknown, code = KNOWLEDGE_DOMAIN_REFERENCED_CODE, status = 409): Promise<unknown> {
  const error = {
    response: {
      status,
      data: { success: false, error: { code, message: 'knowledge domain is referenced by knowledge bases', details: { knowledge_base_count: count } } },
    },
    config: { url: '/api/v1/knowledge-domains/referenced-domain' },
  }
  try { await requestRejectionHandler()(error) }
  catch (rejected) { return rejected }
  assert.fail('HTTP 409 must reject')
}

test('HTTP 409 request errors show the reference count in the current Chinese or English locale', async () => {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const rejected = await rejectedDomainResponse(3)
  assert.equal((rejected as { status: number }).status, 409)
  assert.deepEqual((rejected as { error: { details: unknown } }).error.details, { knowledge_base_count: 3 })
  const format = () => domainErrorMessage(rejected, i18n.global.t('knowledgeDomains.failed'),
    count => i18n.global.t('knowledgeDomains.deleteReferenced', { count }, count))
  assert.equal(format(), '该领域正被 3 个知识库使用，无法删除。')
  i18n.global.locale.value = 'en-US'
  assert.equal(format(), 'This domain is used by 3 knowledge bases and cannot be deleted.')
})

test('English reference messages use singular for one knowledge base', async () => {
  const i18n = createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': enUS } })
  assert.equal(domainErrorMessage(await rejectedDomainResponse(1), i18n.global.t('knowledgeDomains.failed'),
    count => i18n.global.t('knowledgeDomains.deleteReferenced', { count }, count)),
  'This domain is used by 1 knowledge base and cannot be deleted.')
})

test('unknown errors and invalid reference counts keep the server reason or localized fallback', async () => {
  const referencedMessage = () => assert.fail('invalid reference details must not produce a count message')
  const reason = 'knowledge domain is referenced by knowledge bases'
  for (const count of [undefined, null, '3', 0, -1, 2.5, Infinity, Number.MAX_SAFE_INTEGER + 1]) {
    assert.equal(domainErrorMessage(await rejectedDomainResponse(count), '操作失败，请重试。', referencedMessage), reason)
  }
  assert.equal(domainErrorMessage(await rejectedDomainResponse(3, 1005), '操作失败，请重试。', referencedMessage), reason)
  assert.equal(domainErrorMessage(await rejectedDomainResponse(3, KNOWLEDGE_DOMAIN_REFERENCED_CODE, 404), '操作失败，请重试。', referencedMessage), reason)
  assert.equal(domainErrorMessage({ status: 409, error: { code: KNOWLEDGE_DOMAIN_REFERENCED_CODE, details: null } }, '操作失败，请重试。', referencedMessage), '操作失败，请重试。')
  assert.equal(domainErrorMessage({ status: 500, message: 'internal error' }, 'The operation failed. Try again.', referencedMessage), 'internal error')
})

test('missing domain error details retain the localized fallback', () => {
  for (const cause of [null, undefined, {}, { message: '' }, { message: 7 }, { error: {} }]) {
    assert.equal(domainErrorMessage(cause, '操作失败，请重试'), '操作失败，请重试')
  }
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: request rejection localizes registration closure and API key validation`, async () => {
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
    for (const [url, status, reason, key] of [
      ['/api/v1/auth/register', 403, 'Registration is closed', 'auth.registrationClosed'],
      ['/api/v1/tenants/1/api-keys', 400, 'expires_at_unix must be in the future', 'integrations.api.apiKeyExpiresAtInvalid'],
    ] as const) {
      await assert.rejects(requestRejectionHandler(i18n.global.t)({
        config: { url }, response: { status, data: { message: reason, error: { message: reason } } },
      }), error => {
        assert.equal((error as { message: string }).message, i18n.global.t(key))
        assert.notEqual((error as { message: string }).message, reason)
        return true
      })
    }
  })
}
