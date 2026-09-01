import { requestJSON } from './request'
import {
  requireDatabaseStatus,
  requireRecord,
  requireString,
} from './validation'

export function toAdminDatabase(value) {
  const database = requireRecord(value)
  return {
    id: requireString(database.id),
    displayName: requireString(database.displayName),
    internalName: requireString(database.internalName),
    status: requireDatabaseStatus(database.status),
    createdAt: requireString(database.createdAt),
    updatedAt: requireString(database.updatedAt),
  }
}

function toHealth(value) {
  const health = requireRecord(value)
  return {
    status: requireString(health.status),
    metadataDatabase: requireString(health.metadataDatabase),
  }
}

function toAdminStatus(value) {
  const status = requireRecord(value)
  if (typeof status.schemaVersion !== 'number') {
    throw new Error('invalid schema version')
  }
  return {
    service: requireString(status.service),
    apiVersion: requireString(status.apiVersion),
    metadataDatabase: requireString(status.metadataDatabase),
    schemaVersion: status.schemaVersion,
  }
}

export function createAdminApi(requester = requestJSON) {
  return {
    async getHealth() {
      return toHealth(await requester('/health'))
    },

    async getStatus() {
      return toAdminStatus(await requester('/api/v1/status'))
    },

    async getDatabases() {
      const result = await requester('/api/v1/databases')
      if (!Array.isArray(result)) {
        throw new Error('invalid database list')
      }
      return result.map(toAdminDatabase)
    },

    async getDatabase(id) {
      return toAdminDatabase(
        await requester(`/api/v1/databases/${encodeURIComponent(id)}`),
      )
    },

    async createDatabase(displayName) {
      return toAdminDatabase(
        await requester('/api/v1/databases', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ displayName }),
        }),
      )
    },
  }
}

export const adminApi = createAdminApi()
