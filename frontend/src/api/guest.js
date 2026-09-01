import { requestJSON } from './request'
import {
  requireDatabaseStatus,
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
      const result = await requester('/api/v1/guest/databases')
      if (!Array.isArray(result)) {
        throw new Error('invalid guest database list')
      }
      return result.map(toGuestDatabase)
    },
  }
}

export const guestApi = createGuestApi()
