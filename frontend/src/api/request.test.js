import { afterEach, describe, expect, test, vi } from 'vitest'

import { MiniBaseApiError, requestJSON } from './request'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('requestJSON', () => {
  test('does not expose raw backend error bodies', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('internal path /srv/minibase/secrets and stack trace', {
          status: 500,
        }),
      ),
    )

    await expect(requestJSON('/api/v1/status')).rejects.toEqual(
      expect.objectContaining({
        message: 'MiniBase request failed with HTTP 500.',
        status: 500,
      }),
    )
  })

  test('maps known safe error codes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({ error: { code: 'invalid_display_name' } }),
          { status: 400 },
        ),
      ),
    )

    await expect(requestJSON('/api/v1/databases')).rejects.toThrow(
      'Enter a database name between 1 and 200 characters.',
    )
  })

  test('maps database deletion failures to safe messages', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ error: { code: 'database_attached' } }),
          { status: 409 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ error: { code: 'database_unavailable' } }),
          { status: 409 },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ error: { code: 'deletion_failed' } }),
          { status: 500 },
        ),
      )

    vi.stubGlobal('fetch', fetchMock)

    await expect(
      requestJSON('/api/v1/databases/database_test', {
        method: 'DELETE',
      }),
    ).rejects.toThrow(
      'Detach this database from MiniDeploy before deleting it.',
    )

    await expect(
      requestJSON('/api/v1/databases/database_test', {
        method: 'DELETE',
      }),
    ).rejects.toThrow(
      'This database is not currently available for deletion.',
    )

    await expect(
      requestJSON('/api/v1/databases/database_test', {
        method: 'DELETE',
      }),
    ).rejects.toThrow(
      'MiniBase could not delete the database.',
    )
  })

  test('handles network and malformed response failures safely', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('secret detail')))
    await expect(requestJSON('/health')).rejects.toEqual(
      expect.any(MiniBaseApiError),
    )

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('<html>unexpected</html>')),
    )
    await expect(requestJSON('/health')).rejects.toThrow(
      'MiniBase returned an unexpected response.',
    )
  })
})
