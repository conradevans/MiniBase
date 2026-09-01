export function formatTimestamp(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return 'Unavailable'
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

export function formatBytes(value) {
  if (!Number.isFinite(value) || value < 0) {
    return 'Unavailable'
  }
  if (value < 1024) {
    return `${value} B`
  }
  const units = ['KB', 'MB', 'GB', 'TB']
  let size = value
  let unit = -1
  do {
    size /= 1024
    unit += 1
  } while (size >= 1024 && unit < units.length - 1)
  return `${size.toFixed(size >= 10 ? 1 : 2)} ${units[unit]}`
}

export function shortResourceID(value) {
  if (typeof value !== 'string' || value.length <= 18) {
    return value
  }
  return `${value.slice(0, 12)}…${value.slice(-6)}`
}
