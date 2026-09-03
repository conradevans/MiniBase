import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import App from './App'
import { MiniBaseApiError } from './api/request'

const baseDatabase = {
  id: 'database_0123456789abcdef0123456789abcdef',
  displayName: 'Scheduler Production',
  internalName: 'mb_db_0123456789abcdef0123456789abcdef',
  status: 'ready',
  createdAt: '2026-09-01T12:00:00Z',
  updatedAt: '2026-09-01T12:30:00Z',
}

const baseBackup = {
  id: 'backup_0123456789abcdef0123456789abcdef',
  databaseId: baseDatabase.id,
  databaseDisplayName: baseDatabase.displayName,
  kind: 'manual',
  status: 'ready',
  sizeBytes: 2048,
  createdAt: '2026-09-01T12:40:00Z',
  completedAt: '2026-09-01T12:41:00Z',
}

function makeAdminApi(overrides = {}) {
  return {
    getHealth: vi.fn().mockResolvedValue({
      status: 'ok',
      metadataDatabase: 'reachable',
    }),
    getStatus: vi.fn().mockResolvedValue({
      service: 'minibase',
      apiVersion: 'v1',
      metadataDatabase: 'reachable',
      schemaVersion: 3,
    }),
    getDatabases: vi.fn().mockResolvedValue([]),
    getDatabase: vi.fn().mockResolvedValue(baseDatabase),
    createDatabase: vi.fn().mockResolvedValue(baseDatabase),
    deleteDatabase: vi.fn().mockResolvedValue(null),
    getBackups: vi.fn().mockResolvedValue([]),
    getDatabaseBackups: vi.fn().mockResolvedValue([]),
    createBackup: vi.fn().mockResolvedValue({
      id: 'backup_0123456789abcdef0123456789abcdef',
      databaseId: baseDatabase.id,
      databaseDisplayName: baseDatabase.displayName,
      kind: 'manual',
      status: 'ready',
      sizeBytes: 1024,
      createdAt: '2026-09-01T12:40:00Z',
      completedAt: '2026-09-01T12:41:00Z',
    }),
    restoreBackupAsNew: vi.fn().mockResolvedValue(baseDatabase),
    restoreBackupReplace: vi.fn().mockResolvedValue(baseDatabase),
    ...overrides,
  }
}

function makeGuestApi(overrides = {}) {
  return {
    getStatus: vi.fn().mockResolvedValue({ service: 'minibase', status: 'ok' }),
    getDatabases: vi.fn().mockResolvedValue([]),
    ...overrides,
  }
}

function renderApp(pathname, options = {}) {
  return render(
    <App
      initialPathname={pathname}
      adminApi={options.adminApi || makeAdminApi()}
      guestApi={options.guestApi || makeGuestApi()}
    />,
  )
}

describe('MiniBase dashboard', () => {
  test('renders the product landing route', () => {
    renderApp('/')
    expect(
      screen.getByRole('heading', {
        name: 'Manage PostgreSQL without managing PostgreSQL.',
      }),
    ).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Guest View' })).toBeTruthy()
    expect(
      screen.getByRole('link', { name: 'Open Admin Dashboard' }),
    ).toBeTruthy()
  })

  test('renders only allowlisted Guest data', async () => {
    const guestApi = makeGuestApi({
      getDatabases: vi.fn().mockResolvedValue([
        {
          id: baseDatabase.id,
          displayName: 'Guest-safe Database',
          status: 'ready',
          internalName: 'must-not-render',
          roleName: 'must-not-render',
          password: 'highly-sensitive-mock-value',
          credentialPath: '/srv/minibase/secrets/must-not-render',
        },
      ]),
    })
    const { container } = renderApp('/guest', { guestApi })

    expect(await screen.findByText('Guest-safe Database')).toBeTruthy()
    expect(screen.getByText('Ready')).toBeTruthy()
    expect(container.textContent).not.toContain('must-not-render')
    expect(container.textContent).not.toContain('highly-sensitive-mock-value')
    expect(container.textContent).not.toContain('/srv/minibase')
  })

  test('computes real overview counts', async () => {
    const adminApi = makeAdminApi({
      getDatabases: vi.fn().mockResolvedValue([
        baseDatabase,
        { ...baseDatabase, id: 'database_11111111111111111111111111111111', status: 'provisioning' },
        { ...baseDatabase, id: 'database_22222222222222222222222222222222', status: 'error' },
        { ...baseDatabase, id: 'database_33333333333333333333333333333333', status: 'metadata_only' },
      ]),
    })
    renderApp('/admin', { adminApi })

    const summary = await screen.findByLabelText('Database summary')
    expect(within(summary).getByText('Total databases').nextSibling.textContent).toBe('4')
    expect(within(summary).getByText('Ready').nextSibling.textContent).toBe('1')
    expect(within(summary).getByText('Provisioning').nextSibling.textContent).toBe('1')
    expect(within(summary).getByText('Error').nextSibling.textContent).toBe('1')
    expect(screen.getByText('Schema version').nextSibling.textContent).toBe('3')
  })

  test('shows overview loading and safe API failure states', async () => {
    let resolveHealth
    let resolveStatus
    let resolveDatabases
    const adminApi = makeAdminApi({
      getHealth: vi.fn(() => new Promise((resolve) => { resolveHealth = resolve })),
      getStatus: vi.fn(() => new Promise((resolve) => { resolveStatus = resolve })),
      getDatabases: vi.fn(() => new Promise((resolve) => { resolveDatabases = resolve })),
    })
    const view = renderApp('/admin', { adminApi })
    expect(screen.getByText('Loading MiniBase overview…')).toBeTruthy()

    await act(async () => {
      resolveHealth({ status: 'ok', metadataDatabase: 'reachable' })
      resolveStatus({ service: 'minibase', apiVersion: 'v1', metadataDatabase: 'reachable', schemaVersion: 3 })
      resolveDatabases([])
    })
    expect(await screen.findByText('Service status')).toBeTruthy()
    view.unmount()

    const unsafeFailure = makeAdminApi({
      getStatus: vi.fn().mockRejectedValue(new Error('password at /srv/minibase/secrets')),
    })
    const failedView = renderApp('/admin', { adminApi: unsafeFailure })
    expect(await screen.findByText('Unable to load the MiniBase overview.')).toBeTruthy()
    expect(failedView.container.textContent).not.toContain('/srv/minibase')
    expect(failedView.container.textContent).not.toContain('password at')
  })

  test('renders database empty, populated, status, and refresh states', async () => {
    const getDatabases = vi.fn().mockResolvedValue([])
    const adminApi = makeAdminApi({ getDatabases })
    const view = renderApp('/admin/databases', { adminApi })
    expect(await screen.findByText('No databases yet.')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    await waitFor(() => expect(getDatabases).toHaveBeenCalledTimes(2))
    view.unmount()

    const populatedApi = makeAdminApi({
      getDatabases: vi.fn().mockResolvedValue([
        baseDatabase,
        { ...baseDatabase, id: 'database_11111111111111111111111111111111', displayName: 'Building', status: 'provisioning' },
        { ...baseDatabase, id: 'database_22222222222222222222222222222222', displayName: 'Needs attention', status: 'error' },
        { ...baseDatabase, id: 'database_33333333333333333333333333333333', displayName: 'Legacy metadata', status: 'metadata_only' },
      ]),
    })
    renderApp('/admin/databases', { adminApi: populatedApi })
    expect(await screen.findByText('Scheduler Production')).toBeTruthy()
    for (const label of ['Ready', 'Provisioning', 'Error', 'Metadata only']) {
      expect(screen.getByText(label)).toBeTruthy()
    }
  })

  test('requires and trims the create display name', async () => {
    const adminApi = makeAdminApi()
    renderApp('/admin/databases', { adminApi })
    await screen.findByText('No databases yet.')
    fireEvent.click(screen.getAllByRole('button', { name: 'Create Database' })[0])

    const dialog = screen.getByRole('dialog')
    const input = within(dialog).getByLabelText('Display name')
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create database' }))
    expect(await within(dialog).findByText('Display name is required.')).toBeTruthy()
    expect(adminApi.createDatabase).not.toHaveBeenCalled()
  })

  test('prevents duplicate create submissions and never renders returned secrets', async () => {
    let resolveCreate
    const maliciousResult = {
      ...baseDatabase,
      id: 'database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      displayName: 'New Database',
      internalName: 'mb_db_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      roleName: 'must-not-render',
      password: 'mock-secret-must-not-render',
      credentialPath: '/srv/minibase/secrets/must-not-render',
    }
    const getDatabases = vi
      .fn()
      .mockResolvedValueOnce([baseDatabase])
      .mockResolvedValueOnce([baseDatabase, maliciousResult])
    const createDatabase = vi.fn(
      () => new Promise((resolve) => { resolveCreate = resolve }),
    )
    const adminApi = makeAdminApi({ getDatabases, createDatabase })
    const view = renderApp('/admin/databases', { adminApi })
    await screen.findByText('Scheduler Production')
    fireEvent.click(screen.getByRole('button', { name: 'Create Database' }))

    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Display name'), {
      target: { value: '  New Database  ' },
    })
    const submit = within(dialog).getByRole('button', { name: 'Create database' })
    fireEvent.click(submit)
    fireEvent.click(submit)
    expect(createDatabase).toHaveBeenCalledTimes(1)
    expect(createDatabase).toHaveBeenCalledWith('New Database')
    expect(within(dialog).getByRole('button', { name: 'Creating database…' }).disabled).toBe(true)

    await act(async () => {
      resolveCreate(maliciousResult)
    })
    expect(await screen.findByText('New Database was created successfully.')).toBeTruthy()
    expect(view.container.textContent).not.toContain('mock-secret-must-not-render')
    expect(view.container.textContent).not.toContain('/srv/minibase/secrets')
    expect(view.container.textContent).not.toContain('must-not-render')
  })

  test('shows safe create errors', async () => {
    const adminApi = makeAdminApi({
      createDatabase: vi.fn().mockRejectedValue(new Error('credential /srv/minibase/secrets')),
    })
    const view = renderApp('/admin/databases', { adminApi })
    await screen.findByText('No databases yet.')
    fireEvent.click(screen.getAllByRole('button', { name: 'Create Database' })[0])
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'Example' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create database' }))
    expect(await within(dialog).findByText('MiniBase could not create the database.')).toBeTruthy()
    expect(view.container.textContent).not.toContain('/srv/minibase')
  })

  test('renders safe database detail metadata', async () => {
    const adminApi = makeAdminApi({
      getDatabase: vi.fn().mockResolvedValue({
        ...baseDatabase,
        roleName: 'must-not-render',
        password: 'mock-secret-must-not-render',
        credentialPath: '/srv/minibase/secrets/must-not-render',
      }),
    })
    const view = renderApp(`/admin/databases/${baseDatabase.id}`, { adminApi })

    expect(await screen.findByRole('heading', { name: 'Scheduler Production' })).toBeTruthy()
    expect(screen.getAllByText(baseDatabase.internalName).length).toBeGreaterThan(0)
    expect(screen.getByText(baseDatabase.id)).toBeTruthy()
    expect(view.container.textContent).not.toContain('mock-secret-must-not-render')
    expect(view.container.textContent).not.toContain('/srv/minibase/secrets')
    expect(view.container.textContent).not.toContain('must-not-render')
  })


  test('requires the exact database name before deletion is enabled', async () => {
    const deleteDatabase = vi.fn().mockResolvedValue(null)
    const adminApi = makeAdminApi({
      getDatabase: vi.fn().mockResolvedValue({
        ...baseDatabase,
        attachments: [],
      }),
      deleteDatabase,
    })

    renderApp(`/admin/databases/${baseDatabase.id}`, { adminApi })

    await screen.findByRole('heading', {
      name: 'Scheduler Production',
    })

    const openButton = screen.getByRole('button', {
      name: 'Delete database',
    })

    expect(openButton.disabled).toBe(false)
    fireEvent.click(openButton)

    const dialog = screen.getByRole('dialog')
    const confirmation = within(dialog).getByLabelText(
      `Type ${baseDatabase.displayName} to confirm`,
    )
    const submit = within(dialog).getByRole('button', {
      name: 'Delete database',
    })

    expect(submit.disabled).toBe(true)

    fireEvent.change(confirmation, {
      target: { value: 'Scheduler' },
    })
    expect(submit.disabled).toBe(true)
    expect(deleteDatabase).not.toHaveBeenCalled()

    fireEvent.change(confirmation, {
      target: { value: baseDatabase.displayName },
    })
    expect(submit.disabled).toBe(false)
  })

  test('deletes a standalone database and returns to the database list', async () => {
    const deleteDatabase = vi.fn().mockResolvedValue(null)
    const adminApi = makeAdminApi({
      getDatabase: vi.fn().mockResolvedValue({
        ...baseDatabase,
        attachments: [],
      }),
      getDatabases: vi.fn().mockResolvedValue([]),
      deleteDatabase,
    })

    renderApp(`/admin/databases/${baseDatabase.id}`, { adminApi })

    await screen.findByRole('heading', {
      name: 'Scheduler Production',
    })

    fireEvent.click(
      screen.getByRole('button', { name: 'Delete database' }),
    )

    const dialog = screen.getByRole('dialog')

    fireEvent.change(
      within(dialog).getByLabelText(
        `Type ${baseDatabase.displayName} to confirm`,
      ),
      { target: { value: baseDatabase.displayName } },
    )

    fireEvent.click(
      within(dialog).getByRole('button', {
        name: 'Delete database',
      }),
    )

    await waitFor(() => {
      expect(deleteDatabase).toHaveBeenCalledTimes(1)
      expect(deleteDatabase).toHaveBeenCalledWith(baseDatabase.id)
    })

    expect(
      await screen.findByRole('heading', { name: 'Databases' }),
    ).toBeTruthy()
    expect(await screen.findByText('No databases yet.')).toBeTruthy()
  })

  test('disables deletion while a database is attached to MiniDeploy', async () => {
    const attachedDatabase = {
      ...baseDatabase,
      attachments: [
        {
          id: 'attachment_0123456789abcdef0123456789abcdef',
          databaseId: baseDatabase.id,
          consumerType: 'minideploy',
          consumerRef: 'myscheduler',
          bindingName: 'primary',
          createdAt: '2026-09-01T12:00:00Z',
          updatedAt: '2026-09-01T12:00:00Z',
        },
      ],
    }

    const adminApi = makeAdminApi({
      getDatabase: vi.fn().mockResolvedValue(attachedDatabase),
    })

    const view = renderApp(
      `/admin/databases/${baseDatabase.id}`,
      { adminApi },
    )

    await screen.findByRole('heading', {
      name: 'Scheduler Production',
    })

    const deleteButton = screen.getByRole('button', {
      name: 'Delete database',
    })

    expect(deleteButton.disabled).toBe(true)
    expect(view.container.textContent).toContain(
      'Detach it from MiniDeploy before deleting it.',
    )
    expect(view.container.textContent).toContain('myscheduler')
  })

  test('shows a safe refusal if an attachment appears before deletion completes', async () => {
    const deleteDatabase = vi.fn().mockRejectedValue(
      new MiniBaseApiError(
        'Detach this database from MiniDeploy before deleting it.',
        409,
      ),
    )

    const adminApi = makeAdminApi({
      getDatabase: vi.fn().mockResolvedValue({
        ...baseDatabase,
        attachments: [],
      }),
      deleteDatabase,
    })

    renderApp(`/admin/databases/${baseDatabase.id}`, { adminApi })

    await screen.findByRole('heading', {
      name: 'Scheduler Production',
    })

    fireEvent.click(
      screen.getByRole('button', { name: 'Delete database' }),
    )

    const dialog = screen.getByRole('dialog')

    fireEvent.change(
      within(dialog).getByLabelText(
        `Type ${baseDatabase.displayName} to confirm`,
      ),
      { target: { value: baseDatabase.displayName } },
    )

    fireEvent.click(
      within(dialog).getByRole('button', {
        name: 'Delete database',
      }),
    )

    expect(
      await within(dialog).findByText(
        'Detach this database from MiniDeploy before deleting it.',
      ),
    ).toBeTruthy()

    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(deleteDatabase).toHaveBeenCalledTimes(1)
  })

  test('renders real backup empty and populated states without internal fields', async () => {
    const emptyView = renderApp('/admin/backups')
    expect(await screen.findByText('No backups yet. Create a manual backup from a database detail page.')).toBeTruthy()
    expect(screen.getByText('7 daily / 4 weekly')).toBeTruthy()
    expect(screen.getByText('Pending deployment integration')).toBeTruthy()
    emptyView.unmount()

    const adminApi = makeAdminApi({
      getBackups: vi.fn().mockResolvedValue([{
        ...baseBackup,
        path: '/srv/minibase/backups/must-not-render',
        password: 'mock-secret-must-not-render',
      }]),
      getDatabases: vi.fn().mockResolvedValue([baseDatabase]),
    })
    const view = renderApp('/admin/backups', { adminApi })
    expect(await screen.findByText('Scheduler Production')).toBeTruthy()
    expect(screen.getByText('2.00 KB')).toBeTruthy()
    expect(view.container.textContent).not.toContain('/srv/minibase')
    expect(view.container.textContent).not.toContain('mock-secret-must-not-render')
  })

  test('creates a manual backup from database detail with pending and success states', async () => {
    let resolveBackup
    const createBackup = vi.fn(() => new Promise((resolve) => { resolveBackup = resolve }))
    const getDatabaseBackups = vi.fn().mockResolvedValue([])
    const adminApi = makeAdminApi({ createBackup, getDatabaseBackups })
    renderApp(`/admin/databases/${baseDatabase.id}`, { adminApi })
    await screen.findByRole('heading', { name: 'Scheduler Production' })
    const button = await screen.findByRole('button', { name: 'Create Backup' })
    fireEvent.click(button)
    fireEvent.click(button)
    expect(createBackup).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Creating backup…' }).disabled).toBe(true)

    await act(async () => {
      resolveBackup(baseBackup)
    })
    expect(await screen.findByText('Verified manual backup created successfully.')).toBeTruthy()
    expect(getDatabaseBackups).toHaveBeenCalledTimes(2)
  })

  test('restore dialog defaults to safe new-database mode', async () => {
    const restoreBackupAsNew = vi.fn().mockResolvedValue({
      ...baseDatabase,
      id: 'database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      displayName: 'Recovered Database',
    })
    const adminApi = makeAdminApi({
      getBackups: vi.fn().mockResolvedValue([baseBackup]),
      getDatabases: vi.fn().mockResolvedValue([baseDatabase]),
      restoreBackupAsNew,
    })
    renderApp('/admin/backups', { adminApi })
    fireEvent.click(await screen.findByRole('button', { name: 'Restore' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByLabelText('Restore as a new database').checked).toBe(true)
    fireEvent.change(within(dialog).getByLabelText('New database display name'), {
      target: { value: '  Recovered Database  ' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restore as new database' }))
    await waitFor(() => expect(restoreBackupAsNew).toHaveBeenCalledWith(baseBackup.id, 'Recovered Database'))
    expect(await screen.findByText('Recovered Database was restored successfully.')).toBeTruthy()
  })

  test('replace restore requires the exact target name confirmation', async () => {
    const restoreBackupReplace = vi.fn().mockResolvedValue(baseDatabase)
    const adminApi = makeAdminApi({
      getBackups: vi.fn().mockResolvedValue([baseBackup]),
      getDatabases: vi.fn().mockResolvedValue([baseDatabase]),
      restoreBackupReplace,
    })
    renderApp('/admin/backups', { adminApi })
    fireEvent.click(await screen.findByRole('button', { name: 'Restore' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByLabelText('Replace an existing database'))
    expect(within(dialog).getByText(/will be replaced/)).toBeTruthy()
    fireEvent.change(within(dialog).getByLabelText(`Type ${baseDatabase.displayName} to confirm`), {
      target: { value: 'wrong' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Replace database' }))
    expect(await within(dialog).findByText(/exactly to confirm/)).toBeTruthy()
    expect(restoreBackupReplace).not.toHaveBeenCalled()

    fireEvent.change(within(dialog).getByLabelText(`Type ${baseDatabase.displayName} to confirm`), {
      target: { value: baseDatabase.displayName },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Replace database' }))
    await waitFor(() => expect(restoreBackupReplace).toHaveBeenCalledWith(baseBackup.id, baseDatabase.id))
  })

  test('backup errors remain safe', async () => {
    const adminApi = makeAdminApi({
      getBackups: vi.fn().mockRejectedValue(new Error('password /srv/minibase/backups')),
    })
    const view = renderApp('/admin/backups', { adminApi })
    expect(await screen.findByText('Unable to load MiniBase backups.')).toBeTruthy()
    expect(view.container.textContent).not.toContain('/srv/minibase')
    expect(view.container.textContent).not.toContain('password /')
  })

  test('renders a safe missing-database state', async () => {
    const adminApi = makeAdminApi({
      getDatabase: vi.fn().mockRejectedValue(
        new MiniBaseApiError('MiniBase request failed with HTTP 404.', 404),
      ),
    })
    renderApp(`/admin/databases/${baseDatabase.id}`, { adminApi })
    expect(
      await screen.findByRole('heading', { name: 'Database could not be loaded.' }),
    ).toBeTruthy()
    expect(screen.getByText('MiniBase request failed with HTTP 404.')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to databases' })).toBeTruthy()
  })
})
