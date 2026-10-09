import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import test from 'node:test'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'
import { collectLocaleKeys, getLocaleValueAtPath } from './localeKeyAudit'

const removedFeatures = /沙箱|docker\.sock|\b(?:sandbox|wiki|memory|memories|audit|audits|skills?|feishu|IM|CLI|members?|invites?|invitations?|roles?|teams?|organizations?|organisations?)\b|web[ -]?search|internet[ -]?search|search(?:ing)? (?:the )?(?:web|internet)|self_serve|anyone[^.]{0,40}register|公网任何人[^。]*注册|联网搜索|历史对话搜索|chat(?:ting)? history[^.]*semantic search|聊天历史[^。]*搜索|审计|agent.{0,15}(?:memor|记忆)|长期记忆|rag.{0,15}(?:\beval(?:uation)?\b|评测)|技能|飞书|组织|共享空间|shared[ -]?space|成员|邀请|角色|团队/gi

// These words describe retained functions, rather than multi-user permissions.
const allowed: Record<string, string> = {
  'agent.editor.systemPromptPlaceholder': 'The model persona in a system prompt is still configurable.',
  'agentEditor.desc.systemPrompt': 'The role describes a model persona, not a user permission.',
  'knowledgeWorkbench.subtitle': '组织 is a verb meaning organize documents.',
  'agent.subtitle': '组织 is a verb meaning organize documents.',
  'knowledgeList.subtitle': '组织 is a verb meaning organize documents.',
  'knowledgeEditor.document.subtitle': '组织 is a verb meaning organize documents.',
  'system.globalSettings.runtime.queueDescriptions.context': 'Organization means organizing knowledge, not an organization account.',
}

for (const [locale, bundle] of Object.entries({ 'en-US': enUS, 'zh-CN': zhCN })) {
  test(`${locale}: locale values contain no deleted feature descriptions`, () => {
    const unexpected: string[] = []
    for (const key of collectLocaleKeys(bundle)) {
      const value = getLocaleValueAtPath(bundle, key) as string
      removedFeatures.lastIndex = 0
      for (const match of value.matchAll(removedFeatures)) {
        const permittedWord = /^(?:role|角色|组织|organization)$/i.test(match[0])
        if (!allowed[key] || !permittedWord) unexpected.push(`${key}: ${match[0]}`)
      }
    }
    assert.deepEqual(unexpected, [], unexpected.join('\n'))
  })
}

test('settings templates contain no hard-coded descriptions of removed features', () => {
  const unexpected: string[] = []
  function walk(directory: URL) {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
      if (entry.isDirectory()) { walk(path); continue }
      if (!entry.name.endsWith('.vue')) continue
      const source = readFileSync(path, 'utf8').replace(/<!--[\s\S]*?-->/g, '')
      const template = source.match(/<template>([\s\S]*?)<script/)?.[1] || ''
      // Accessibility role attributes refer to semantic HTML and remain valid.
      const visible = template.replace(/\brole="[^"]*"/g, '')
      removedFeatures.lastIndex = 0
      if (removedFeatures.test(visible)) unexpected.push(path.pathname)
    }
  }
  walk(new URL('../views/settings/', import.meta.url))
  walk(new URL('../components/settings/', import.meta.url))
  walk(new URL('../views/system/', import.meta.url))
  walk(new URL('../views/integrations/', import.meta.url))
  assert.deepEqual(unexpected, [])
})

test('unused generic high-risk confirmation is removed while SSRF entry confirmations remain', () => {
  const source = readFileSync(new URL('../views/system/SystemSettings.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /highRiskConfirmBody|confirm\.highRiskBody/)
  assert.match(source, /system\.globalSettings\.listConfirm\./)
  for (const action of ['add', 'remove']) {
    assert.ok(source.includes(`confirmSsrfListEntryChange('${action}', entry)`))
  }
  for (const bundle of [enUS, zhCN]) {
    assert.equal('highRiskBody' in bundle.system.globalSettings.confirm, false)
    assert.equal('bodyAuthRegistrationMode' in bundle.system.globalSettings.confirm, false)
    for (const action of ['add', 'remove'] as const) {
      const confirmation = bundle.system.globalSettings.listConfirm.ssrf.whitelist[action]
      assert.match(confirmation.body, /\{entry\}/)
      assert.ok(confirmation.confirmBtn)
    }
  }
})
