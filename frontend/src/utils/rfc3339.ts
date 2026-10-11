type Timestamp = { seconds: bigint; fraction: string }

function parseRFC3339(value: string): Timestamp | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?([Zz]|[+-]\d{2}:\d{2})$/.exec(value)
  if (!match) return null
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fraction = '', zone] = match
  const year = Number(yearText), month = Number(monthText), day = Number(dayText)
  const hour = Number(hourText), minute = Number(minuteText), second = Number(secondText)
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const monthDays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  if (month < 1 || month > 12 || day < 1 || day > monthDays[month - 1] || hour > 23 || minute > 59 || second > 59) return null
  let offset = 0
  if (!/^[Zz]$/.test(zone)) {
    const offsetHour = Number(zone.slice(1, 3)), offsetMinute = Number(zone.slice(4, 6))
    if (offsetHour > 23 || offsetMinute > 59) return null
    offset = (zone[0] === '+' ? 1 : -1) * (offsetHour * 60 + offsetMinute) * 60
  }
  const date = new Date(0)
  date.setUTCFullYear(year, month - 1, day)
  date.setUTCHours(hour, minute, second, 0)
  return { seconds: BigInt(date.getTime() / 1000) - BigInt(offset), fraction }
}

// Keep fractional seconds intact when comparing PostgreSQL checkpoints.
export function compareRFC3339(left: string, right: string): -1 | 0 | 1 | null {
  const a = parseRFC3339(left), b = parseRFC3339(right)
  if (!a || !b) return null
  if (a.seconds !== b.seconds) return a.seconds > b.seconds ? 1 : -1
  const width = Math.max(a.fraction.length, b.fraction.length)
  const aFraction = a.fraction.padEnd(width, '0'), bFraction = b.fraction.padEnd(width, '0')
  return aFraction === bFraction ? 0 : aFraction > bFraction ? 1 : -1
}

export function isRFC3339After(left: string, right: string): boolean {
  return compareRFC3339(left, right) === 1
}
