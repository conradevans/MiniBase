import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import DatabasesPage from './DatabasesPage'

afterEach(cleanup)

const ready = {
  id: 'database_0123456789abcdef0123456789abcdef',
  displayName: 'Ready Database',
  internalName: 'mb_db_0123456789abcdef0123456789abcdef',
  status: 'ready',
  guestVisible: false,
  createdAt: '2026-09-17T00:00:00Z',
  updatedAt: '2026-09-17T00:00:00Z',
}

describe('DatabasesPage Explorer entry point', () => {
  test('offers Explore only for ready databases without redesigning status content', async () => {
    const provisioning = {
      ...ready,
      id: 'database_11111111111111111111111111111111',
      displayName: 'Provisioning Database',
      internalName: 'mb_db_11111111111111111111111111111111',
      status: 'provisioning',
    }
    render(
      <DatabasesPage
        api={{
          getDatabases: vi.fn().mockResolvedValue([ready, provisioning]),
          createDatabase: vi.fn(),
        }}
        navigate={vi.fn()}
      />,
    )

    expect(await screen.findByText('Ready Database')).toBeTruthy()
    expect(screen.getByText('Provisioning Database')).toBeTruthy()
    expect(screen.getByText('Ready')).toBeTruthy()
    expect(screen.getByText('Provisioning')).toBeTruthy()
    const exploreLinks = screen.getAllByRole('link', { name: 'Explore' })
    expect(exploreLinks).toHaveLength(1)
    expect(exploreLinks[0].getAttribute('href')).toBe(
      `/admin/databases/${ready.id}/explorer`,
    )
  })
})
