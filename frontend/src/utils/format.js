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

export function shortResourceID(value) {
  if (typeof value !== 'string' || value.length <= 18) {
    return value
  }
  return `${value.slice(0, 12)}…${value.slice(-6)}`
}
