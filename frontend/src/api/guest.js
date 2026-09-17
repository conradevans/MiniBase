import { requestJSON } from './request'
import {
  requireDatabaseStatus,
  requireNumber,
  requireRecord,
  requireString,
} from './validation'

export function toGuestDatabase(value) {
  const database = requireRecord(value)
  return {
    id: requireString(database.id),
    displayName: requireString(database.displayName),
    status: requireDatabaseStatus(database.status),
  }
}

function requireCount(value) {
  const count = requireNumber(value)
  if (!Number.isInteger(count) || count < 0) {
    throw new Error('invalid guest database count')
  }
  return count
}

export function toGuestDatabasesResponse(value) {
  const result = requireRecord(value)
  const summaryValue = requireRecord(result.summary)
  if (!Array.isArray(result.databases)) {
    throw new Error('invalid guest database list')
  }
  const summary = {
    total: requireCount(summaryValue.total),
    showing: requireCount(summaryValue.showing),
    hidden: requireCount(summaryValue.hidden),
  }
  const databases = result.databases.map(toGuestDatabase)
  if (
    summary.total !== summary.showing + summary.hidden ||
    summary.showing !== databases.length
  ) {
    throw new Error('invalid guest database summary')
  }
  return { summary, databases }
}

export function createGuestApi(requester = requestJSON) {
  return {
    async getStatus() {
      const value = requireRecord(await requester('/api/v1/guest/status'))
      return {
        service: requireString(value.service),
        status: requireString(value.status),
      }
    },

    async getDatabases() {
      return toGuestDatabasesResponse(
        await requester('/api/v1/guest/databases'),
      )
    },
  }
}

export const guestApi = createGuestApi()
