import { requestJSON } from './request'
import {
  requireBackupKind,
  requireBackupStatus,
  requireBoolean,
  requireDatabaseStatus,
  requireNullableString,
  requireNumber,
  requireRecord,
  requireString,
} from './validation'

function toAdminAttachment(value) {
  const attachment = requireRecord(value)
  return {
    id: requireString(attachment.id),
    databaseId: requireString(attachment.databaseId),
    consumerType: requireString(attachment.consumerType),
    consumerRef: requireString(attachment.consumerRef),
    bindingName: requireString(attachment.bindingName),
    createdAt: requireString(attachment.createdAt),
    updatedAt: requireString(attachment.updatedAt),
  }
}

export function toAdminDatabase(value) {
  const database = requireRecord(value)
  return {
    id: requireString(database.id),
    displayName: requireString(database.displayName),
    internalName: requireString(database.internalName),
    status: requireDatabaseStatus(database.status),
    createdAt: requireString(database.createdAt),
    updatedAt: requireString(database.updatedAt),
    attachments: Array.isArray(database.attachments)
      ? database.attachments.map(toAdminAttachment)
      : [],
  }
}

export function toMiniDeployDeployment(value) {
  const deployment = requireRecord(value)

  return {
    app: requireString(deployment.app),
    supported: requireBoolean(deployment.supported),
    status: requireString(deployment.status),
    databaseAttached: requireBoolean(deployment.databaseAttached),
    databaseDetached: requireBoolean(deployment.databaseDetached),
    databaseId: deployment.databaseId === undefined
      ? ''
      : requireString(deployment.databaseId),
  }
}

export function toAdminBackup(value) {
  const backup = requireRecord(value)
  return {
    id: requireString(backup.id),
    databaseId: requireString(backup.databaseId),
    databaseDisplayName: requireString(backup.databaseDisplayName),
    kind: requireBackupKind(backup.kind),
    status: requireBackupStatus(backup.status),
    sizeBytes: requireNumber(backup.sizeBytes),
    createdAt: requireString(backup.createdAt),
    completedAt: requireNullableString(backup.completedAt),
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

    async deleteDatabase(id) {
      return requester(`/api/v1/databases/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      })
    },

    async getDeployments() {
      const result = await requester('/api/v1/deployments')
      if (!Array.isArray(result)) {
        throw new Error('invalid deployment list')
      }
      return result.map(toMiniDeployDeployment)
    },

    async attachDatabase(id, app) {
      return toAdminDatabase(
        await requester(
          `/api/v1/databases/${encodeURIComponent(id)}/attach`,
          {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
            },
            body: JSON.stringify({ app }),
          },
        ),
      )
    },

    async detachDatabase(id) {
      return toAdminDatabase(
        await requester(
          `/api/v1/databases/${encodeURIComponent(id)}/detach`,
          {
            method: 'POST',
          },
        ),
      )
    },

    async getBackups() {
      const result = await requester('/api/v1/backups')
      if (!Array.isArray(result)) {
        throw new Error('invalid backup list')
      }
      return result.map(toAdminBackup)
    },

    async getDatabaseBackups(id) {
      const result = await requester(
        `/api/v1/databases/${encodeURIComponent(id)}/backups`,
      )
      if (!Array.isArray(result)) {
        throw new Error('invalid backup list')
      }
      return result.map(toAdminBackup)
    },

    async createBackup(databaseId) {
      return toAdminBackup(
        await requester(
          `/api/v1/databases/${encodeURIComponent(databaseId)}/backups`,
          { method: 'POST' },
        ),
      )
    },

    async restoreBackupAsNew(backupId, displayName) {
      return toAdminDatabase(
        await requester(
          `/api/v1/backups/${encodeURIComponent(backupId)}/restore`,
          {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mode: 'new', displayName }),
          },
        ),
      )
    },

    async restoreBackupReplace(backupId, targetDatabaseId) {
      return toAdminDatabase(
        await requester(
          `/api/v1/backups/${encodeURIComponent(backupId)}/restore`,
          {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ mode: 'replace', targetDatabaseId }),
          },
        ),
      )
    },
  }
}

export const adminApi = createAdminApi()
