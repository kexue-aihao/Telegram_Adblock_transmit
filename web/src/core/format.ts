// Formatting is frozen: the panel reports every timestamp in UTC and the tests
// pin the rendered text, so these factories must not be "modernised".
const numberFormat = new Intl.NumberFormat("zh-CN")
const axisNumberFormat = new Intl.NumberFormat("zh-CN", { notation: "compact", maximumFractionDigits: 1 })
const dateFormat = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "UTC",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
})

export function num(value: number | null | undefined): string {
  return numberFormat.format(value || 0)
}

export function compactNum(value: number): string {
  return axisNumberFormat.format(value)
}

export function date(value: string | number | Date | null | undefined): string {
  const parsed = new Date(value as string)
  return Number.isNaN(parsed.getTime()) ? "未知时间" : dateFormat.format(parsed)
}

export function utcDay(offset = 0): string {
  const now = new Date()
  now.setUTCDate(now.getUTCDate() + offset)
  return now.toISOString().slice(0, 10)
}

export function positiveInt(raw: string | null | undefined, fallback = 1): number {
  return /^[1-9]\d*$/.test(raw || "") && Number.isSafeInteger(Number(raw)) ? Number(raw) : fallback
}

// localDay renders a UTC timestamp as the YYYY-MM-DD value a date input needs.
export function utcDayOf(value: string): string {
  return value.slice(0, 10)
}
