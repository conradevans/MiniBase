import { describe, expect, test } from 'vitest'

import {
  databaseDetailPath,
  databaseExplorerPath,
  resolveRoute,
} from './routing'

describe('frontend routing', () => {
  test('resolves every product route including Database Explorer', () => {
    expect(resolveRoute('/')).toEqual({ screen: 'landing' })
    expect(resolveRoute('/guest/')).toEqual({ screen: 'guest' })
    expect(resolveRoute('/admin')).toEqual({ screen: 'overview' })
    expect(resolveRoute('/admin/databases/')).toEqual({ screen: 'databases' })
    expect(resolveRoute('/admin/visibility/')).toEqual({ screen: 'visibility' })
    expect(resolveRoute('/admin/backups/')).toEqual({ screen: 'backups' })
    expect(resolveRoute('/admin/activity/')).toEqual({ screen: 'activity' })
    expect(
      resolveRoute('/admin/databases/database_0123456789abcdef0123456789abcdef'),
    ).toEqual({
      screen: 'database-detail',
      databaseID: 'database_0123456789abcdef0123456789abcdef',
    })
    expect(
      resolveRoute('/admin/databases/database_0123456789abcdef0123456789abcdef/explorer/'),
    ).toEqual({
      screen: 'database-explorer',
      databaseID: 'database_0123456789abcdef0123456789abcdef',
    })
  })

  test('encodes and decodes database IDs safely', () => {
    const databaseID = 'database_0123456789abcdef0123456789abcdef'
    expect(databaseDetailPath(databaseID)).toBe(
      `/admin/databases/${databaseID}`,
    )
    expect(databaseExplorerPath(databaseID)).toBe(
      `/admin/databases/${databaseID}/explorer`,
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
    expect(resolveRoute('/admin/databases/id/explorer/query')).toEqual({
      screen: 'not-found',
    })
  })
})
