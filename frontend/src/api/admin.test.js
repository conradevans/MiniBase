import { describe, expect, test, vi } from 'vitest'

import { createAdminApi, toAdminBackup, toAdminDatabase } from './admin'

const database = {
  id: 'database_0123456789abcdef0123456789abcdef',
  displayName: 'Scheduler',
  internalName: 'mb_db_0123456789abcdef0123456789abcdef',
  status: 'ready',
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
  roleName: 'must-not-survive',
  password: 'must-not-survive',
  credentialPath: '/must-not-survive',
}

const backup = {
  id: 'backup_0123456789abcdef0123456789abcdef',
  databaseId: database.id,
  databaseDisplayName: database.displayName,
  kind: 'manual',
  status: 'ready',
  sizeBytes: 42,
  createdAt: '2026-09-01T01:00:00Z',
  completedAt: '2026-09-01T01:01:00Z',
  path: '/must-not-survive',
  password: 'must-not-survive',
}

describe('admin API', () => {
  test('allowlists database response fields', () => {
    expect(Object.keys(toAdminDatabase(database))).toEqual([
      'id',
      'displayName',
      'internalName',
      'status',
      'createdAt',
      'updatedAt',
    ])
  })

  test('uses the existing API for health, list, detail, and create', async () => {
    const requester = vi
      .fn()
      .mockResolvedValueOnce({ status: 'ok', metadataDatabase: 'reachable' })
      .mockResolvedValueOnce([database])
      .mockResolvedValueOnce(database)
      .mockResolvedValueOnce(database)
    const api = createAdminApi(requester)

    await expect(api.getHealth()).resolves.toEqual({
      status: 'ok',
      metadataDatabase: 'reachable',
    })
    await expect(api.getDatabases()).resolves.toEqual([toAdminDatabase(database)])
    await expect(api.getDatabase(database.id)).resolves.toEqual(
      toAdminDatabase(database),
    )
    await expect(api.createDatabase('Scheduler')).resolves.toEqual(
      toAdminDatabase(database),
    )

    expect(requester).toHaveBeenNthCalledWith(1, '/health')
    expect(requester).toHaveBeenNthCalledWith(2, '/api/v1/databases')
    expect(requester).toHaveBeenNthCalledWith(
      3,
      `/api/v1/databases/${database.id}`,
    )
    expect(requester).toHaveBeenNthCalledWith(4, '/api/v1/databases', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ displayName: 'Scheduler' }),
    })
  })


  test('allowlists backup fields and uses explicit backup API requests', async () => {
    expect(Object.keys(toAdminBackup(backup))).toEqual([
      'id',
      'databaseId',
      'databaseDisplayName',
      'kind',
      'status',
      'sizeBytes',
      'createdAt',
      'completedAt',
    ])
    const requester = vi.fn()
      .mockResolvedValueOnce([backup])
      .mockResolvedValueOnce([backup])
      .mockResolvedValueOnce(backup)
      .mockResolvedValueOnce(database)
      .mockResolvedValueOnce(database)
    const api = createAdminApi(requester)

    await api.getBackups()
    await api.getDatabaseBackups(database.id)
    await api.createBackup(database.id)
    await api.restoreBackupAsNew(backup.id, 'Recovered')
    await api.restoreBackupReplace(backup.id, database.id)

    expect(requester).toHaveBeenNthCalledWith(1, '/api/v1/backups')
    expect(requester).toHaveBeenNthCalledWith(2, `/api/v1/databases/${database.id}/backups`)
    expect(requester).toHaveBeenNthCalledWith(3, `/api/v1/databases/${database.id}/backups`, { method: 'POST' })
    expect(requester).toHaveBeenNthCalledWith(4, `/api/v1/backups/${backup.id}/restore`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: 'new', displayName: 'Recovered' }),
    })
    expect(requester).toHaveBeenNthCalledWith(5, `/api/v1/backups/${backup.id}/restore`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: 'replace', targetDatabaseId: database.id }),
    })
  })

  test('rejects malformed administrative database responses', () => {
    expect(() => toAdminDatabase({ ...database, status: 'deleted' })).toThrow(
      'unexpected response',
    )
  })
})
