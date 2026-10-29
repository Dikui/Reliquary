import assert from 'node:assert/strict'
import test from 'node:test'
import { chartGeometry, mapTrend } from './trend'
import type { CallSummary } from '../../api/insights'
const row = (extra: Partial<CallSummary> = {}): CallSummary => ({ model_id: '', purpose: '', period: '2026-10-01T00:00:00Z', currency: 'USD', calls: 10, failures: 2, failure_rate: .2, average_duration_ms: 100, input_tokens: 40, output_tokens: 10, cost: .0004, unknown_input_calls: 0, unknown_output_calls: 0, unknown_cost_calls: 0, not_applicable_cost_calls: 0, ...extra })
test('stacked calls and ticks use the same scale, including zero and fractional costs', () => {
  const points = mapTrend([row(), row({ period: '2026-10-02T00:00:00Z', calls: 0, failures: 0 })], 'calls')[0]!.points
  const plot = chartGeometry(points, 10)
  assert.equal(plot.bars[0]!.successHeight, 140)
  assert.equal(plot.bars[0]!.failureHeight, 35)
  assert.equal(plot.bars[0]!.failureY, 35)
  assert.equal(plot.bars[1]!.successHeight, 0)
  assert.equal(plot.ticks.at(-1)!.value, 10)
  assert.equal(plot.ticks.at(-1)!.y, plot.bars[0]!.failureY)
  const cost = chartGeometry(mapTrend([row()], 'cost')[0]!.points, .0008)
  assert.equal(cost.bars[0]!.successHeight, 87.5)
  assert.equal(cost.bars[0]!.failureHeight, 0, 'aggregate API cannot split cost by result')
  assert.equal(cost.bars[0]!.y, cost.ticks[2]!.y)
})
test('missing days are zero while null or partially known values retain accounting meaning', () => {
  const series = mapTrend([row({ cost: null, unknown_cost_calls: 10 }), row({ period: '2026-10-03T00:00:00Z', unknown_cost_calls: 2 })], 'cost')[0]!
  assert.deepEqual(series.points.map(p => p.value), [null, 0, .0004])
  const plot = chartGeometry(series.points, .0004)
  assert.equal(plot.bars[0]!.y, null)
  assert.equal(plot.bars[2]!.unknown, 2)
})
test('very small costs keep the axis maximum aligned with the top of the bar', () => {
  const plot = chartGeometry(mapTrend([row({ cost: 1e-15 })], 'cost')[0]!.points, 1e-15)
  assert.equal(plot.bars[0]!.y, 35)
  assert.equal(plot.ticks.at(-1)!.y, 35)
  assert.equal(plot.ticks.at(-1)!.value, 1e-15)
})
