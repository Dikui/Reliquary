import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import type { ShelfCertificationStatus, ShelfCondition } from '../api/shelf'
import en from '../i18n/locales/en-US'
import zh from '../i18n/locales/zh-CN'
import { certificationStatusKey, groupCertifications, summarizeCertificationProgress } from './shelfCertification'

test('认证状态在两种语言中有对应文案，旧记录显示未记录', () => {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } })
  const cases: [ShelfCertificationStatus | undefined, string, string][] = [
    ['obtained', '已取证', 'Obtained'],
    ['in_progress', '进行中', 'In progress'],
    ['not_started', '待启动', 'Not started'],
    ['delayed', '延期', 'Delayed'],
    ['paused', '暂停', 'Paused'],
    ['date_conflict', '日期待核对', 'Date needs review'],
    ['unknown', '未记录', 'Not recorded'],
    [undefined, '未记录', 'Not recorded'],
  ]
  for (const [status, chinese, english] of cases) {
    const key = certificationStatusKey(status)
    assert.equal(i18n.global.t(key, {}, { locale: 'zh-CN' }), chinese)
    assert.equal(i18n.global.t(key, {}, { locale: 'en-US' }), english)
  }
})

test('电芯配置总体进度按延期、待核对和全部取证规则汇总', () => {
  const summarize = (...statuses: (ShelfCertificationStatus | undefined)[]) =>
    summarizeCertificationProgress(statuses.map(status => ({ status })))

  assert.deepEqual(summarize('obtained', 'obtained'), { status: 'completed', delayedCount: 0 })
  assert.deepEqual(summarize('date_conflict', 'delayed', 'obtained', 'delayed'), { status: 'date_conflict', delayedCount: 2 })
  assert.deepEqual(summarize('delayed', 'obtained', 'delayed'), { status: 'delayed', delayedCount: 2 })
  assert.deepEqual(summarize('paused', 'date_conflict'), { status: 'date_conflict', delayedCount: 0 })
  assert.deepEqual(summarize('obtained', 'not_started', 'paused', 'unknown', undefined), { status: 'in_progress', delayedCount: 0 })
  assert.deepEqual(summarize(), { status: 'in_progress', delayedCount: 0 })
})

test('电芯和适用条件相同的认证跨来源归组，条件与选项顺序不影响分组', () => {
  const record = (origin: string, cell_part_id: string | undefined, conditions: ShelfCondition[] | null) => ({
    origin,
    certification: { owner_id: origin, standard: 'standard', certification_object: 'model', region: '', cell_part_id, conditions },
  })
  const condition = { module_type: 'cooling', part_ids: ['liquid', 'air'] }
  const pack = { module_type: 'pack', part_ids: ['pack-a'] }
  const model = record('self', 'cell-a', [condition, pack])
  const component = record('component', 'cell-a', [pack, { ...condition, part_ids: ['air', 'liquid'] }])
  const differentCell = record('ancestor', 'cell-b', [condition, pack])
  const differentConditions = record('ancestor', 'cell-a', [pack])
  const noConditions = record('self', undefined, null)
  const emptyConditions = record('ancestor', '', [])

  const groups = groupCertifications([model, component, differentCell, differentConditions, noConditions, emptyConditions])
  assert.deepEqual(groups.map(group => group.rows), [[model, component], [differentCell], [differentConditions], [noConditions, emptyConditions]])
})
