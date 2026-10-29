import type { CallSummary } from '../../api/insights'
export type TrendMetric = 'calls' | 'tokens' | 'cost'
export interface TrendPoint { date: string; value: number | null; unknown: number; notApplicable: number; failures?: number }
export function trendValue(row: CallSummary, metric: TrendMetric): TrendPoint {
  const value = metric === 'calls' ? row.calls : metric === 'cost' ? row.cost : row.input_tokens == null && row.output_tokens == null ? null : (row.input_tokens || 0) + (row.output_tokens || 0)
  return { date: row.period.slice(0, 10), value, failures: metric === 'calls' ? row.failures : 0, unknown: metric === 'cost' ? row.unknown_cost_calls : metric === 'tokens' ? row.unknown_input_calls + row.unknown_output_calls : 0, notApplicable: metric === 'cost' ? row.not_applicable_cost_calls : 0 }
}
export function mapTrend(rows: CallSummary[], metric: TrendMetric) {
  const dates = [...new Set(rows.map(row => row.period.slice(0, 10)))].sort()
  // Fill missing UTC days with zero; an existing null bucket remains unknown.
  if (dates.length > 1) {
    const first = Date.parse(dates[0]!), last = Date.parse(dates.at(-1)!)
    if ((last - first) / 86400000 <= 3660) {
      dates.length = 0
      for (let date = first; date <= last; date += 86400000) dates.push(new Date(date).toISOString().slice(0, 10))
    }
  }
  const currencies = [...new Set(rows.map(row => row.currency))].sort()
  return currencies.map(currency => ({ currency, points: dates.map(date => {
    const row = rows.find(row => row.currency === currency && row.period.slice(0, 10) === date)
    return row ? trendValue(row, metric) : { date, value: 0, unknown: 0, notApplicable: 0 }
  }) }))
}
export function chartGeometry(points: TrendPoint[], maximum: number) {
  const baseline = 210, height = 175, left = 70, width = 634
  const domain = maximum > 0 ? maximum : 1
  const scale = height / domain
  const step = width / Math.max(1, points.length)
  const barWidth = Math.min(32, step * .65)
  return {
    ticks: [0, .25, .5, .75, 1].map(fraction => ({ value: domain * fraction, y: baseline - height * fraction })),
    bars: points.map((point, index) => {
      const total = Math.max(0, point.value ?? 0)
      const failed = Math.max(0, Math.min(total, point.failures ?? 0))
      return { ...point, x: left + step * (index + .5) - barWidth / 2, width: barWidth,
        y: point.value === null ? null : baseline - total * scale,
        successY: baseline - (total - failed) * scale, successHeight: (total - failed) * scale,
        failureY: baseline - total * scale, failureHeight: failed * scale }
    }),
  }
}
