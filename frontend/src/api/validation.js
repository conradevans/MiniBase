import { MiniBaseApiError } from './request'

const databaseStatuses = new Set([
  'metadata_only',
  'provisioning',
  'ready',
  'error',
])

export function requireRecord(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
  return value
}

export function requireString(value) {
  if (typeof value !== 'string') {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
  return value
}

export function requireDatabaseStatus(value) {
  if (!databaseStatuses.has(value)) {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
  return value
}
