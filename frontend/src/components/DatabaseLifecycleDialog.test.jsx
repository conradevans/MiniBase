import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import DatabaseLifecycleDialog from './DatabaseLifecycleDialog'

const database = {
  id: 'database_0123456789abcdef0123456789abcdef',
  displayName: 'Lifecycle Test',
}

describe('DatabaseLifecycleDialog', () => {
  test('only offers compatible deployments for attachment', async () => {
    const onAttach = vi.fn().mockResolvedValue(undefined)

    render(
      <DatabaseLifecycleDialog
        mode="attach"
        database={database}
        deployments={[
          {
            app: 'running-app',
            supported: true,
            status: 'running',
            databaseAttached: false,
            databaseDetached: false,
            databaseId: '',
          },
          {
            app: 'detached-app',
            supported: true,
            status: 'database-detached',
            databaseAttached: false,
            databaseDetached: true,
            databaseId: '',
          },
          {
            app: 'already-attached',
            supported: true,
            status: 'running',
            databaseAttached: true,
            databaseDetached: false,
            databaseId: database.id,
          },
          {
            app: 'unsupported-app',
            supported: false,
            status: 'running',
            databaseAttached: false,
            databaseDetached: false,
            databaseId: '',
          },
        ]}
        onClose={() => {}}
        onAttach={onAttach}
        onDetach={() => {}}
      />,
    )

    const select = screen.getByLabelText('MiniDeploy deployment')

    expect(screen.getByText(/running-app/)).toBeTruthy()
    expect(screen.getByText(/detached-app/)).toBeTruthy()
    expect(
      screen.queryByText(/already-attached/),
    ).toBeNull()
    expect(
      screen.queryByText(/unsupported-app/),
    ).toBeNull()

    fireEvent.change(select, {
      target: { value: 'detached-app' },
    })

    fireEvent.click(
      screen.getByRole('button', {
        name: 'Attach database',
      }),
    )

    await waitFor(() => {
      expect(onAttach).toHaveBeenCalledWith(
        'detached-app',
      )
    })
  })

  test('explains that detach preserves the database and stops the app', async () => {
    const onDetach = vi.fn().mockResolvedValue(undefined)

    render(
      <DatabaseLifecycleDialog
        mode="detach"
        database={database}
        attachment={{
          consumerRef: 'scheduler',
        }}
        deployments={[]}
        onClose={() => {}}
        onAttach={() => {}}
        onDetach={onDetach}
      />,
    )

    expect(
      screen.getByText(/stop that deployment/i),
    ).toBeTruthy()

    expect(
      screen.getByText(/database, schema, data/i),
    ).toBeTruthy()

    fireEvent.click(
      screen.getByRole('button', {
        name: 'Detach database',
      }),
    )

    await waitFor(() => {
      expect(onDetach).toHaveBeenCalledTimes(1)
    })
  })
})
