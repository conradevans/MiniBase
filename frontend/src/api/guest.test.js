import { describe, expect, test, vi } from 'vitest'

import {
  createGuestApi,
  toGuestDatabase,
  toGuestDatabasesResponse,
} from './guest'

describe('guest API', () => {
  test('allowlists exactly id, displayName, and status', () => {
    const safe = toGuestDatabase({
      id: 'database_0123456789abcdef0123456789abcdef',
      displayName: 'Public summary',
      status: 'ready',
      internalName: 'must-not-survive',
      roleName: 'must-not-survive',
      password: 'must-not-survive',
    })
    expect(safe).toEqual({
      id: 'database_0123456789abcdef0123456789abcdef',
      displayName: 'Public summary',
      status: 'ready',
    })
    expect(Object.keys(safe)).toEqual(['id', 'displayName', 'status'])
  })

  test('requests only dedicated guest endpoints', async () => {
    const response = {
      summary: { total: 2, showing: 1, hidden: 1 },
      databases: [{
        id: 'database_0123456789abcdef0123456789abcdef',
        displayName: 'Public summary',
        status: 'ready',
        internalName: 'must-not-survive',
      }],
    }
    const requester = vi
      .fn()
      .mockResolvedValueOnce({ service: 'minibase', status: 'ok' })
      .mockResolvedValueOnce(response)
    const api = createGuestApi(requester)

    await api.getStatus()
    await expect(api.getDatabases()).resolves.toEqual({
      summary: response.summary,
      databases: [{
        id: response.databases[0].id,
        displayName: 'Public summary',
        status: 'ready',
      }],
    })

    expect(requester).toHaveBeenNthCalledWith(1, '/api/v1/guest/status')
    expect(requester).toHaveBeenNthCalledWith(2, '/api/v1/guest/databases')
  })

  test('rejects inconsistent or malformed aggregate envelopes', () => {
    expect(() => toGuestDatabasesResponse({
      summary: { total: 2, showing: 1, hidden: 0 },
      databases: [],
    })).toThrow()
    expect(() => toGuestDatabasesResponse({
      summary: { total: 0, showing: -1, hidden: 1 },
      databases: [],
    })).toThrow()
    expect(() => toGuestDatabasesResponse([])).toThrow()
  })
})
