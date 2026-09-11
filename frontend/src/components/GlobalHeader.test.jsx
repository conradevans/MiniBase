import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import GlobalHeader from './GlobalHeader'

describe('GlobalHeader', () => {
  test('uses the required product order and shared administrator session card', () => {
    const { rerender } = render(<GlobalHeader mode="root" navigate={vi.fn()} />)
    expect(
      within(screen.getByRole('navigation')).getAllByRole('link').map((link) => link.textContent),
    ).toEqual(['ReactorLab', 'MiniDeploy', 'MiniBase'])

    rerender(
      <GlobalHeader
        mode="admin"
        navigate={vi.fn()}
        sessionLabel="Admin · admin@example.com"
      />,
    )
    expect(
      within(screen.getByRole('navigation')).getAllByRole('link').map((link) => link.textContent),
    ).toEqual(['ReactorLab', 'MiniDeploy', 'MiniBase', 'MiniAI'])
    expect(screen.getByText('ACCESS SESSION').tagName).toBe('SMALL')
    expect(screen.getByText('admin@example.com').tagName).toBe('STRONG')
    expect(
      screen.getByLabelText('Access session: admin@example.com'),
    ).toBeTruthy()
  })

  test('uses the safe administrator fallback without inventing an email', () => {
    render(
      <GlobalHeader mode="admin" navigate={vi.fn()} sessionLabel="Admin" />,
    )
    expect(screen.getByText('Administrator')).toBeTruthy()
  })

  test('opens and closes the mobile menu by selection, outside press, and Escape', () => {
    const navigate = vi.fn()
    render(
      <GlobalHeader
        mode="guest"
        navigate={navigate}
        sessionLabel="Guest View"
      />,
    )
    const menuButton = screen.getByRole('button', { name: 'Open product menu' })

    fireEvent.click(menuButton)
    const menu = document.getElementById('minibase-product-menu')
    expect(within(menu).getByText('Guest View')).toBeTruthy()
    fireEvent.click(within(menu).getByText('Switch Access'))
    expect(navigate).toHaveBeenCalledWith('/')
    expect(document.getElementById('minibase-product-menu')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Open product menu' }))
    fireEvent.pointerDown(document.body)
    expect(document.getElementById('minibase-product-menu')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Open product menu' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(document.getElementById('minibase-product-menu')).toBeNull()
    expect(document.activeElement).toBe(menuButton)
  })
})
