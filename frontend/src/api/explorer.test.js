import { describe, expect, test, vi } from 'vitest'

import {
  createExplorerMethods,
  toExplorerCatalog,
  toExplorerDescription,
  toExplorerRowsPage,
} from './explorer'

const databaseID = 'database_0123456789abcdef0123456789abcdef'

const catalog = {
  schemas: [{
    name: 'public',
    objects: [
      { name: 'users', type: 'table', password: 'must-not-survive' },
      { name: 'active_users', type: 'view' },
      { name: 'monthly', type: 'materialized_view' },
    ],
    credentialPath: '/must-not-survive',
  }],
  databaseUrl: 'must-not-survive',
}

const description = {
  schema: 'public',
  object: { name: 'users', type: 'table' },
  columns: [{
    name: 'id',
    dataType: 'bigint',
    nullable: false,
    primaryKey: true,
    ordinalPosition: 1,
    defaultExpression: 'must-not-survive',
  }],
}

const page = {
  schema: 'public',
  object: { name: 'users', type: 'table' },
  columns: ['id', 'optional', 'payload', 'blob'],
  rows: [[
    { kind: 'value', value: '1', truncated: false },
    { kind: 'null', truncated: false },
    { kind: 'json', value: '{"ok":true}', truncated: true },
    { kind: 'binary', value: '<binary: 128 bytes>', truncated: false },
  ]],
  limit: 50,
  offset: 0,
  hasMore: true,
  sql: 'must-not-survive',
}

describe('Explorer API', () => {
  test('allowlists catalog, column, row, and cell response fields', () => {
    expect(toExplorerCatalog(catalog)).toEqual({
      schemas: [{
        name: 'public',
        objects: [
          { name: 'users', type: 'table' },
          { name: 'active_users', type: 'view' },
          { name: 'monthly', type: 'materialized_view' },
        ],
      }],
    })
    expect(toExplorerDescription(description)).toEqual({
      schema: 'public',
      object: { name: 'users', type: 'table' },
      columns: [{
        name: 'id',
        dataType: 'bigint',
        nullable: false,
        primaryKey: true,
        ordinalPosition: 1,
      }],
    })
    expect(toExplorerRowsPage(page)).toEqual({
      schema: 'public',
      object: { name: 'users', type: 'table' },
      columns: page.columns,
      rows: [[
        { kind: 'value', value: '1', truncated: false },
        { kind: 'null', value: '', truncated: false },
        { kind: 'json', value: '{"ok":true}', truncated: true },
        { kind: 'binary', value: '<binary: 128 bytes>', truncated: false },
      ]],
      limit: 50,
      offset: 0,
      hasMore: true,
    })
  })

  test('uses only structured protected Explorer GET routes', async () => {
    const requester = vi.fn()
      .mockResolvedValueOnce(catalog)
      .mockResolvedValueOnce(description)
      .mockResolvedValueOnce(page)
    const api = createExplorerMethods(requester)

    await api.getExplorerObjects(databaseID)
    await api.getExplorerColumns(databaseID, 'Odd Schema', 'select table')
    await api.getExplorerRows(databaseID, 'Odd Schema', 'select table', 25, 50)

    expect(requester).toHaveBeenNthCalledWith(
      1,
      `/api/v1/databases/${databaseID}/explorer/objects`,
    )
    expect(requester.mock.calls[1][0]).toBe(
      `/api/v1/databases/${databaseID}/explorer/columns?schema=Odd+Schema&object=select+table`,
    )
    expect(requester.mock.calls[2][0]).toBe(
      `/api/v1/databases/${databaseID}/explorer/rows?schema=Odd+Schema&object=select+table&limit=25&offset=50`,
    )
    for (const call of requester.mock.calls) {
      expect(call).toHaveLength(1)
      expect(call[0]).not.toContain('/query')
    }
  })

  test('rejects malformed object types, cells, and row shapes', () => {
    expect(() => toExplorerCatalog({
      schemas: [{ name: 'public', objects: [{ name: 'users', type: 'sequence' }] }],
    })).toThrow()
    expect(() => toExplorerRowsPage({
      ...page,
      rows: [[{ kind: 'value', value: '1', truncated: false }]],
    })).toThrow()
    expect(() => toExplorerRowsPage({
      ...page,
      rows: [[
        { kind: 'sql', value: 'SELECT 1', truncated: false },
        ...page.rows[0].slice(1),
      ]],
    })).toThrow()
  })
})
