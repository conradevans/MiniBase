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

const activityTypes = new Set([
  'database_create',
  'database_delete',
  'backup_create',
  'backup_restore_new',
  'backup_restore_replace',
  'attachment_attach',
  'attachment_detach',
  'automatic_backup',
  'retention_prune',
])

const activityOutcomes = new Set([
  'success',
  'failure',
  'blocked',
])

const activitySources = new Set([
  'admin',
  'minideploy',
  'system',
])

function requireActivityValue(value, allowed) {
  const result = requireString(value)
  if (!allowed.has(result)) {
    throw new Error('unexpected response')
  }
  return result
}

export function toAdminActivityEvent(value) {
  const event = requireRecord(value)

  return {
    databaseId: event.databaseId === undefined
      ? ''
      : requireString(event.databaseId),
    databaseDisplayName:
      event.databaseDisplayName === undefined
        ? ''
        : requireString(event.databaseDisplayName),
    type: requireActivityValue(
      event.type,
      activityTypes,
    ),
    outcome: requireActivityValue(
      event.outcome,
      activityOutcomes,
    ),
    source: requireActivityValue(
      event.source,
      activitySources,
    ),
    detail: requireString(event.detail),
    createdAt: requireString(event.createdAt),
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

function toAdminSession(value) {
  const session = requireRecord(value)
  const mode = requireString(session.mode)

  if (mode === 'access') {
    return {
      mode,
      email: requireString(session.email),
    }
  }

  if (mode === 'local') {
    return {
      mode,
      email: '',
    }
  }

  throw new Error('unexpected response')
}

export function createAdminApi(requester = requestJSON) {
  return {
    async getSession() {
      return toAdminSession(
        await requester('/api/v1/session'),
      )
    },

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

    async getActivity() {
      const result = await requester('/api/v1/activity')
      if (!Array.isArray(result)) {
        throw new Error('invalid activity list')
      }
      return result.map(toAdminActivityEvent)
    },

    async getDatabaseActivity(id) {
      const result = await requester(
        `/api/v1/databases/${encodeURIComponent(id)}/activity`,
      )
      if (!Array.isArray(result)) {
        throw new Error('invalid activity list')
      }
      return result.map(toAdminActivityEvent)
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
