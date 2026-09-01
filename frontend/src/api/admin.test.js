import { describe, expect, test, vi } from 'vitest'

import { createAdminApi, toAdminDatabase } from './admin'

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

  test('rejects malformed administrative database responses', () => {
    expect(() => toAdminDatabase({ ...database, status: 'deleted' })).toThrow(
      'unexpected response',
    )
  })
})
