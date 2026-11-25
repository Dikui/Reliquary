import assert from 'node:assert/strict'
import { test } from 'node:test'
import { certificationStatus, checklistTone, expandShelf, filterShelf, findCountryRegion, snapshotDate, summarizeShelf } from '../src/data/shelf'
import type { SaleStatus, ShelfModel, ShelfSnapshot } from '../src/data/shelf'

const mainland = { key: 'cn', name: '中国大陆' }
const japan = { key: 'jp', name: '日本' }
const australia = { key: 'au', name: '澳洲' }
const ul = { key: 'ul', name: 'UL系列标准适用地区' }
const iec = { key: 'iec', name: 'IEC 系列标准适用地区' }
const line = { id: 'line-a', name: '产品线 A' }

function sale(fields: Partial<SaleStatus> = {}): SaleStatus {
  return {
    region: mainland.key, region_name: mainland.name, status: 'GA', raw: 'GA（已确认）',
    notes: '', planned_eom: false, conditional: false, ...fields,
  }
}

function model(fields: Partial<ShelfModel> = {}): ShelfModel {
  return {
    id: 'model-a', name: 'Test-A-123', product_name: '测试产品', platform: '',
    product_line: line, current_stage: '', sale_statuses: [], checklist: [], certifications: [], ...fields,
  }
}

function snapshot(...models: ShelfModel[]): ShelfSnapshot {
  return {
    library: { id: 'library', name: '测试库' },
    as_of: '2026-11-25T08:00:00Z',
    regions: [mainland, japan, australia, ul, iec],
    product_lines: [line],
    models,
  }
}

// 02-showcase-frontend · small spec tests for the showcase snapshot mapping.
test('each sale-status record becomes a row with its original status text', () => {
  const rows = expandShelf(snapshot(model({
    sale_statuses: [sale(), sale({ region: japan.key, region_name: japan.name, status: 'DEV', raw: '研发中' })],
  })))
  assert.deepEqual(rows.map((row) => [row.model.id, row.regionLabel, row.status.key, row.statusLabel]), [
    ['model-a', '中国大陆', 'GA', 'GA（已确认）'],
    ['model-a', '日本', 'DEV', '研发中'],
  ])
  assert.notEqual(rows[0].id, rows[1].id)
})

test('a model with no sale statuses stays in the catalog as an unrecorded row', () => {
  const rows = expandShelf(snapshot(model()))
  assert.equal(rows.length, 1)
  assert.equal(rows[0].regionKey, '')
  assert.equal(rows[0].regionLabel, '未记录')
  assert.equal(rows[0].statusLabel, '未记录')
  assert.deepEqual(expandShelf(snapshot()), [])
})

test('blank sale label fields fall back to the configured region name and canonical status', () => {
  const [row] = expandShelf(snapshot(model({ sale_statuses: [sale({ region_name: '', raw: '' })] })))
  assert.equal(row.regionLabel, mainland.name)
  assert.equal(row.statusLabel, 'GA')
})

test('country lookup maps configured region names to their independent API keys', () => {
  const regions = snapshot().regions
  for (const [code, region] of [['CN', mainland], ['JP', japan], ['AU', australia], ['US', ul], ['DE', iec]] as const) {
    assert.equal(findCountryRegion(regions, code), region)
  }
  assert.equal(findCountryRegion([{ key: 'other', name: '其他' }], 'JP'), undefined)
})

test('country lookup also matches region keys, standard names and IEC name prefixes', () => {
  const regions = [
    { key: 'gb', name: 'GB' },
    { key: 'iec-eu', name: 'IEC-欧洲（含英国、瑞士）' },
    { key: 'jp', name: 'JP' },
  ]
  assert.equal(findCountryRegion(regions, 'CN'), regions[0])
  assert.equal(findCountryRegion(regions, 'DE'), regions[1])
  assert.equal(findCountryRegion(regions, 'JP'), regions[2])
  assert.equal(findCountryRegion(regions, 'AU'), undefined)
})

test('catalog filters combine region, product line, canonical status and name search', () => {
  const rows = expandShelf(snapshot(
    model({ sale_statuses: [sale(), sale({ region: japan.key })] }),
    model({ id: 'model-b', sale_statuses: [sale({ status: 'DEV', raw: 'GA 后复核' })] }),
    model({ id: 'model-c', product_line: { id: 'other', name: line.name }, sale_statuses: [sale()] }),
    model({ id: 'model-d', name: 'Different', sale_statuses: [sale()] }),
  ))
  const matching = filterShelf(rows, { region: mainland.key, productLine: line.id, status: 'GA', query: 'Ｔｅｓｔ-ａ 测试' })
  assert.deepEqual(matching.map((row) => [row.model.id, row.regionKey]), [['model-a', 'cn']])
})

test('market and status totals count expanded records by key', () => {
  const rows = expandShelf(snapshot(
    model({ sale_statuses: [sale(), sale({ region: japan.key, status: 'Planning', raw: 'GA 预估' })] }),
    model({ id: 'model-b' }),
  ))
  const summary = summarizeShelf(rows)
  assert.deepEqual([...summary.byRegion], [['cn', 1], ['jp', 1]])
  assert.deepEqual([...summary.byStatus], [['GA', 1], ['Planning', 1], ['unrecorded', 1]])
})

test('certification statuses use the Chinese display labels', () => {
  for (const [key, label] of [
    ['obtained', '已取证'], ['in_progress', '办理中'], ['paused', '暂停'],
    ['delayed', '延期'], ['not_started', '未开始'], ['date_conflict', '日期冲突'], ['unknown', '未知'], ['', '未知'],
  ]) {
    assert.equal(certificationStatus(key).label, label)
  }
})

test('a checklist gap stays highlighted when a file exists', () => {
  assert.equal(checklistTone({ name: '测试资料', status: 'missing', status_label: '缺', gap: true, has_file: true }), 'amber')
})

test('the displayed snapshot date uses the calendar date in as_of', () => {
  assert.equal(snapshotDate('2026-11-25T23:50:00-05:00'), '2026-11-25')
  assert.equal(snapshotDate(''), '')
})
