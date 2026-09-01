import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import DatabaseDetailPage from './DatabaseDetailPage'

const database = {
  id: 'database_0123456789abcdef0123456789abcdef',
  displayName: 'Scheduler Production',
  internalName: 'mb_db_0123456789abcdef0123456789abcdef',
  status: 'ready',
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T00:00:00Z',
}

function apiFor(value) {
  return {
    getDatabase: vi.fn().mockResolvedValue(value),
    getDatabaseBackups: vi.fn().mockResolvedValue([]),
    getDatabases: vi.fn().mockResolvedValue([database]),
  }
}

describe('MiniDeploy attachment visibility', () => {
  test('shows safe attachment information and drops injected secret fields', async () => {
    const api = apiFor({
      ...database,
      attachments: [{
        id: 'attachment_0123456789abcdef0123456789abcdef',
        databaseId: database.id,
        consumerType: 'minideploy',
        consumerRef: 'myscheduler',
        bindingName: 'primary',
        createdAt: '2026-09-01T01:00:00Z',
        updatedAt: '2026-09-01T01:00:00Z',
        password: 'mock-secret-must-not-render',
        databaseUrl: 'postgresql://must-not-render',
      }],
    })
    const view = render(
      <DatabaseDetailPage api={api} databaseID={database.id} navigate={() => {}} />,
    )

    expect(await screen.findByText('Attached application')).toBeTruthy()
    expect(screen.getByText('myscheduler')).toBeTruthy()
    expect(screen.getByText('MiniDeploy')).toBeTruthy()
    expect(screen.getByText('Primary')).toBeTruthy()
    expect(view.container.textContent).not.toContain('mock-secret-must-not-render')
    expect(view.container.textContent).not.toContain('postgresql://')
  })

  test('shows the deliberate no-attachment state', async () => {
    render(
      <DatabaseDetailPage api={apiFor(database)} databaseID={database.id} navigate={() => {}} />,
    )
    expect(await screen.findByText('No MiniDeploy application is attached to this database.')).toBeTruthy()
  })
})
