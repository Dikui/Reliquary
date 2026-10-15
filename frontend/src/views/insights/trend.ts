import type { CallSummary } from '../../api/insights'
export type TrendMetric = 'calls' | 'tokens' | 'cost'
export interface TrendPoint { date: string; value: number | null; unknown: number; notApplicable: number }
export function trendValue(row: CallSummary, metric: TrendMetric): TrendPoint {
  const value = metric === 'calls' ? row.calls : metric === 'cost' ? row.cost : row.input_tokens == null && row.output_tokens == null ? null : (row.input_tokens || 0) + (row.output_tokens || 0)
  return { date: row.period.slice(0, 10), value, unknown: metric === 'cost' ? row.unknown_cost_calls : metric === 'tokens' ? row.unknown_input_calls + row.unknown_output_calls : 0, notApplicable: metric === 'cost' ? row.not_applicable_cost_calls : 0 }
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
  const x = (index: number) => 54 + index * 650 / Math.max(1, points.length - 1)
  const y = (value: number) => 210 - value * 175 / Math.max(1e-12, maximum)
  const segments: string[] = []
  let segment: string[] = []
  const flush = () => { if (segment.length) segments.push(segment.join(' ')); segment = [] }
  points.forEach((point, index) => { if (point.value == null) flush(); else segment.push(`${x(index)},${y(point.value)}`) })
  flush()
  return { segments, points: points.map((point, index) => ({ ...point, x: x(index), y: point.value == null ? null : y(point.value) })) }
}
