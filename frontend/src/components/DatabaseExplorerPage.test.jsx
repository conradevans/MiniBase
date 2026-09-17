import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { MiniBaseApiError } from '../api/request'
import DatabaseExplorerPage from './DatabaseExplorerPage'

afterEach(cleanup)

const databaseID = 'database_0123456789abcdef0123456789abcdef'
const database = {
  id: databaseID,
  displayName: 'Scheduler Production',
  internalName: 'mb_db_0123456789abcdef0123456789abcdef',
  status: 'ready',
  guestVisible: false,
  createdAt: '2026-09-17T00:00:00Z',
  updatedAt: '2026-09-17T00:00:00Z',
}

const catalog = {
  schemas: [
    {
      name: 'analytics',
      objects: [{ name: 'monthly', type: 'materialized_view' }],
    },
    {
      name: 'public',
      objects: [
        { name: 'active_users', type: 'view' },
        { name: 'users', type: 'table' },
      ],
    },
  ],
}

function description(schema = 'analytics', object = catalog.schemas[0].objects[0]) {
  return {
    schema,
    object,
    columns: [
      {
        name: 'id',
        dataType: 'bigint',
        nullable: false,
        primaryKey: true,
        ordinalPosition: 1,
      },
      {
        name: 'payload',
        dataType: 'jsonb',
        nullable: true,
        primaryKey: false,
        ordinalPosition: 2,
      },
      {
        name: 'blob',
        dataType: 'bytea',
        nullable: true,
        primaryKey: false,
        ordinalPosition: 3,
      },
      {
        name: 'large_text',
        dataType: 'text',
        nullable: false,
        primaryKey: false,
        ordinalPosition: 4,
      },
    ],
  }
}

function rowsPage({
  schema = 'analytics',
  object = catalog.schemas[0].objects[0],
  limit = 50,
  offset = 0,
  hasMore = true,
} = {}) {
  return {
    schema,
    object,
    columns: ['id', 'payload', 'blob', 'large_text'],
    rows: [[
      { kind: 'value', value: '1', truncated: false },
      { kind: 'null', value: '', truncated: false },
      { kind: 'binary', value: '<binary: 18432 bytes>', truncated: false },
      { kind: 'value', value: 'bounded text', truncated: true },
    ]],
    limit,
    offset,
    hasMore,
  }
}

function makeApi(overrides = {}) {
  return {
    getDatabase: vi.fn().mockResolvedValue(database),
    getExplorerObjects: vi.fn().mockResolvedValue(catalog),
    getExplorerColumns: vi.fn().mockResolvedValue(description()),
    getExplorerRows: vi.fn().mockResolvedValue(rowsPage()),
    ...overrides,
  }
}

describe('DatabaseExplorerPage', () => {
  test('renders schemas, object types, columns, data, and read-only controls', async () => {
    const api = makeApi()
    render(
      <DatabaseExplorerPage api={api} databaseID={databaseID} navigate={vi.fn()} />,
    )

    expect(await screen.findByRole('heading', { name: 'Scheduler Production' })).toBeTruthy()
    expect(screen.getByText('READ ONLY')).toBeTruthy()
    expect(screen.getByText('analytics')).toBeTruthy()
    expect(screen.getByText('public')).toBeTruthy()
    expect(screen.getAllByText('MATERIALIZED VIEW').length).toBeGreaterThan(0)
    expect(screen.getByText('VIEW')).toBeTruthy()
    expect(screen.getByText('TABLE')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'analytics.monthly' })).toBeTruthy()
    expect(await screen.findByText('NULL')).toBeTruthy()
    expect(screen.getByText('<binary: 18432 bytes>')).toBeTruthy()
    expect(screen.getByText('… [truncated]')).toBeTruthy()
    expect(screen.getByTestId('explorer-data-grid').className).toContain('explorer-table-scroll')
    expect(api.getExplorerRows).toHaveBeenCalledWith(
      databaseID,
      'analytics',
      'monthly',
      50,
      0,
    )

    fireEvent.click(screen.getByRole('tab', { name: 'Columns' }))
    expect(await screen.findByText('Primary Key')).toBeTruthy()
    expect(screen.getByText('PK')).toBeTruthy()
    expect(screen.getByText('jsonb')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /insert/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /delete row/i })).toBeNull()
    expect(screen.queryByText(/sql console/i)).toBeNull()
  })

  test('switches objects and paginates through bounded page sizes', async () => {
    const getExplorerColumns = vi.fn((_, schema, name) => Promise.resolve(
      description(schema, { name, type: name === 'users' ? 'table' : 'view' }),
    ))
    const getExplorerRows = vi.fn((_, schema, name, limit, offset) => Promise.resolve(
      rowsPage({
        schema,
        object: { name, type: name === 'users' ? 'table' : 'view' },
        limit,
        offset,
        hasMore: offset === 0,
      }),
    ))
    const api = makeApi({ getExplorerColumns, getExplorerRows })
    render(
      <DatabaseExplorerPage api={api} databaseID={databaseID} navigate={vi.fn()} />,
    )

    await screen.findByRole('heading', { name: 'analytics.monthly' })
    fireEvent.click(screen.getByRole('button', { name: /usersTABLE/ }))
    expect(await screen.findByRole('heading', { name: 'public.users' })).toBeTruthy()
    await waitFor(() => expect(getExplorerRows).toHaveBeenCalledWith(
      databaseID, 'public', 'users', 50, 0,
    ))

    fireEvent.change(screen.getByLabelText('Rows per page'), {
      target: { value: '25' },
    })
    await waitFor(() => expect(getExplorerRows).toHaveBeenCalledWith(
      databaseID, 'public', 'users', 25, 0,
    ))
    fireEvent.click(screen.getByRole('button', { name: 'Next →' }))
    await waitFor(() => expect(getExplorerRows).toHaveBeenCalledWith(
      databaseID, 'public', 'users', 25, 25,
    ))
    expect(await screen.findByText('Showing 26–26')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '← Previous' }))
    await waitFor(() => expect(getExplorerRows).toHaveBeenLastCalledWith(
      databaseID, 'public', 'users', 25, 0,
    ))
  })

  test('handles empty, unavailable, and safe failure states', async () => {
    const emptyApi = makeApi({
      getExplorerObjects: vi.fn().mockResolvedValue({
        schemas: [{ name: 'public', objects: [] }],
      }),
    })
    const emptyView = render(
      <DatabaseExplorerPage api={emptyApi} databaseID={databaseID} navigate={vi.fn()} />,
    )
    expect(await screen.findByText('No tables or views are available.')).toBeTruthy()
    expect(screen.getByText('No supported objects')).toBeTruthy()
    emptyView.unmount()

    const unavailableApi = makeApi({
      getDatabase: vi.fn().mockResolvedValue({ ...database, status: 'provisioning' }),
    })
    const unavailableView = render(
      <DatabaseExplorerPage api={unavailableApi} databaseID={databaseID} navigate={vi.fn()} />,
    )
    expect(await screen.findByText(/available only when this database is ready/)).toBeTruthy()
    expect(unavailableApi.getExplorerObjects).not.toHaveBeenCalled()
    unavailableView.unmount()

    const failedApi = makeApi({
      getExplorerObjects: vi.fn().mockRejectedValue(
        new MiniBaseApiError('Database Explorer is temporarily unavailable.', 503),
      ),
    })
    const failedView = render(
      <DatabaseExplorerPage api={failedApi} databaseID={databaseID} navigate={vi.fn()} />,
    )
    expect(await screen.findByText('Database Explorer is temporarily unavailable.')).toBeTruthy()
    expect(failedView.container.textContent).not.toContain('/srv/minibase')
  })
})
