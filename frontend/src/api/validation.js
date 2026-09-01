import { MiniBaseApiError } from './request'

const backupKinds = new Set(['manual', 'automatic', 'pre_restore'])
const backupStatuses = new Set(['creating', 'ready', 'error'])

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

export function requireBackupKind(value) {
  if (!backupKinds.has(value)) {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
  return value
}

export function requireBackupStatus(value) {
  if (!backupStatuses.has(value)) {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
  return value
}

export function requireNumber(value) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
  return value
}

export function requireNullableString(value) {
  if (value !== null && typeof value !== 'string') {
    throw new MiniBaseApiError('MiniBase returned an unexpected response.')
  }
  return value
}
