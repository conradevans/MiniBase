import { describe, expect, test, vi } from 'vitest'

import { createGuestApi, toGuestDatabase } from './guest'

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
    const requester = vi
      .fn()
      .mockResolvedValueOnce({ service: 'minibase', status: 'ok' })
      .mockResolvedValueOnce([])
    const api = createGuestApi(requester)

    await api.getStatus()
    await api.getDatabases()

    expect(requester).toHaveBeenNthCalledWith(1, '/api/v1/guest/status')
    expect(requester).toHaveBeenNthCalledWith(2, '/api/v1/guest/databases')
  })
})
