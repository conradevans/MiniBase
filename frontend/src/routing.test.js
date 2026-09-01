import { describe, expect, test } from 'vitest'

import { databaseDetailPath, resolveRoute } from './routing'

describe('frontend routing', () => {
  test('resolves every Phase 4 product route', () => {
    expect(resolveRoute('/')).toEqual({ screen: 'landing' })
    expect(resolveRoute('/guest/')).toEqual({ screen: 'guest' })
    expect(resolveRoute('/admin')).toEqual({ screen: 'overview' })
    expect(resolveRoute('/admin/databases/')).toEqual({ screen: 'databases' })
    expect(
      resolveRoute('/admin/databases/database_0123456789abcdef0123456789abcdef'),
    ).toEqual({
      screen: 'database-detail',
      databaseID: 'database_0123456789abcdef0123456789abcdef',
    })
  })

  test('encodes and decodes database IDs safely', () => {
    const path = databaseDetailPath('database_0123456789abcdef0123456789abcdef')
    expect(path).toBe(
      '/admin/databases/database_0123456789abcdef0123456789abcdef',
    )
    expect(resolveRoute('/admin/databases/%2E%2E')).toEqual({
      screen: 'not-found',
    })
    expect(resolveRoute('/admin/databases/a%2Fb')).toEqual({
      screen: 'not-found',
    })
  })

  test('rejects unknown and nested routes', () => {
    expect(resolveRoute('/backups')).toEqual({ screen: 'not-found' })
    expect(resolveRoute('/admin/databases/id/extra')).toEqual({
      screen: 'not-found',
    })
  })
})
