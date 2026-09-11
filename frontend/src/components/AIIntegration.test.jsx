import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import {
  CONVERT_APP_PROMPT,
  NEW_APP_PROMPT,
} from '../minibasePrompts'
import AIIntegration from './AIIntegration'

describe('AIIntegration', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  test('copies each substantive MiniBase integration prompt', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    render(<AIIntegration />)

    fireEvent.click(screen.getByRole('button', { name: 'Copy New App prompt' }))
    expect(writeText).toHaveBeenCalledWith(NEW_APP_PROMPT)

    fireEvent.click(screen.getByRole('button', { name: 'Copy Convert Existing App prompt' }))
    expect(writeText).toHaveBeenLastCalledWith(CONVERT_APP_PROMPT)
  })

  test('shows Copied for three seconds and then restores the label', async () => {
    vi.useFakeTimers()
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    })
    render(<AIIntegration />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy New App prompt' }))
    })
    expect(screen.getByText('Copied')).toBeTruthy()

    act(() => {
      vi.advanceTimersByTime(3000)
    })
    expect(screen.queryByText('Copied')).toBeNull()
  })
})
