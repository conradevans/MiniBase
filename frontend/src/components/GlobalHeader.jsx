import { useEffect, useRef, useState } from 'react'

import AppLink from './AppLink'
import Brand from './Brand'
import ProductNav from './ProductNav'

function adminSessionValue(sessionLabel) {
  const prefix = 'Admin · '
  if (sessionLabel.startsWith(prefix)) {
    return sessionLabel.slice(prefix.length).trim() || 'Administrator'
  }
  return 'Administrator'
}

function SessionControl({ mode, sessionLabel }) {
  if (mode === 'admin') {
    const value = adminSessionValue(sessionLabel)
    return (
      <div
        className="access-session-card"
        title={value}
        aria-label={`Access session: ${value}`}
      >
        <span className="access-session-dot" aria-hidden="true" />
        <span className="access-session-copy">
          <small>ACCESS SESSION</small>
          <strong>{value}</strong>
        </span>
      </div>
    )
  }

  return (
    <div className="current-session" title={sessionLabel}>
      {sessionLabel}
    </div>
  )
}

export default function GlobalHeader({
  mode = 'root',
  navigate,
  sessionLabel = '',
}) {
  const [open, setOpen] = useState(false)
  const headerRef = useRef(null)
  const menuButtonRef = useRef(null)
  const hasSession = mode !== 'root' && sessionLabel

  useEffect(() => {
    if (!open) return undefined

    function handlePointerDown(event) {
      if (!headerRef.current?.contains(event.target)) setOpen(false)
    }

    function handleKeyDown(event) {
      if (event.key === 'Escape') {
        setOpen(false)
        menuButtonRef.current?.focus()
      }
    }

    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  function closeMenu() {
    setOpen(false)
  }

  const switchAccess = (
    <AppLink
      className="button secondary switch-access-button"
      href="/"
      navigate={navigate}
      onClick={closeMenu}
    >
      Switch Access
    </AppLink>
  )

  return (
    <header className="global-header" ref={headerRef}>
      <Brand navigate={navigate} />

      <div className="global-header-desktop">
        <ProductNav mode={mode} />
        {hasSession ? (
          <>
            {switchAccess}
            <SessionControl mode={mode} sessionLabel={sessionLabel} />
          </>
        ) : null}
      </div>

      <button
        ref={menuButtonRef}
        className="mobile-menu-button"
        type="button"
        aria-label={open ? 'Close product menu' : 'Open product menu'}
        aria-expanded={open}
        aria-controls="minibase-product-menu"
        onClick={() => setOpen((current) => !current)}
      >
        <span aria-hidden="true">{open ? '×' : '☰'}</span>
      </button>

      {open ? (
        <div className="mobile-product-menu" id="minibase-product-menu">
          <ProductNav mode={mode} onNavigate={closeMenu} />
          {hasSession ? (
            <div className="mobile-access-controls">
              <AppLink
                className="mobile-switch-access"
                href="/"
                navigate={navigate}
                onClick={closeMenu}
              >
                Switch Access
              </AppLink>
              <SessionControl mode={mode} sessionLabel={sessionLabel} />
            </div>
          ) : null}
        </div>
      ) : null}
    </header>
  )
}
