/** Convert a local form value, including a calendar day, to an instant. */
export function localDateTimeISO(value: string): string {
  return new Date(/^\d{4}-\d{2}-\d{2}$/.test(value) ? `${value}T00:00:00` : value).toISOString()
}

/** Format instants in the viewer's zone, preserving date-only calendar values. */
export function formatDateTime(
  value: string | undefined | null,
  locale: string,
  options: Intl.DateTimeFormatOptions = { dateStyle: 'medium' },
  fallback = '',
): string {
  if (!value) return fallback
  const calendar = /^\d{4}-\d{2}-\d{2}$/.test(value)
  const date = new Date(calendar ? `${value}T00:00:00Z` : value)
  if (!Number.isFinite(date.getTime()) || (calendar && date.toISOString().slice(0, 10) !== value)) return fallback
  if (!calendar) return new Intl.DateTimeFormat(locale, options).format(date)
  const { timeStyle: _timeStyle, hour: _hour, minute: _minute, second: _second,
    fractionalSecondDigits: _fraction, dayPeriod: _period, timeZoneName: _zoneName, ...dateOptions } = options
  return new Intl.DateTimeFormat(locale, { ...dateOptions, timeZone: 'UTC' }).format(date)
}
