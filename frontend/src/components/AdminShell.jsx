import { useEffect, useState } from 'react'

import AppLink from './AppLink'
import Brand from './Brand'

export default function AdminShell({
  active,
  navigate,
  api,
  children,
}) {
  const [session, setSession] = useState(null)

  useEffect(() => {
    const timeout = window.setTimeout(async () => {
      try {
        setSession(await api.getSession())
      } catch {
        setSession(null)
      }
    }, 0)

    return () => window.clearTimeout(timeout)
  }, [api])

  const localSession = session?.mode === 'local'

  return (
    <main className="admin-page">
      <div className="app-shell">
        <header className="topbar">
          <Brand
            navigate={navigate}
            subtitle="Database control plane"
          />

          <div className="control-plane-state">
            <span
              className="status-dot status-ready"
              aria-hidden="true"
            />
            <span>
              <small>
                {localSession
                  ? 'LOCAL ACCESS'
                  : 'ACCESS SESSION'}
              </small>
              <strong>
                {localSession
                  ? 'Local administrator'
                  : session?.email ||
                    'Authenticated administrator'}
              </strong>
            </span>
          </div>
        </header>

        <div className="admin-grid">
          <aside className="sidebar">
            <nav aria-label="MiniBase navigation">
              <AppLink
                className={
                  active === 'overview'
                    ? 'nav-item active'
                    : 'nav-item'
                }
                href="/admin"
                navigate={navigate}
              >
                Overview
              </AppLink>

              <AppLink
                className={
                  active === 'databases'
                    ? 'nav-item active'
                    : 'nav-item'
                }
                href="/admin/databases"
                navigate={navigate}
              >
                Databases
              </AppLink>

              <AppLink
                className={
                  active === 'backups'
                    ? 'nav-item active'
                    : 'nav-item'
                }
                href="/admin/backups"
                navigate={navigate}
              >
                Backups
              </AppLink>

              <span
                className="nav-item unavailable"
                aria-disabled="true"
              >
                Activity <small>Later</small>
              </span>
            </nav>

            <p className="sidebar-note">
              Public administrator routes require Cloudflare Access.
              Private control-plane access remains loopback-only.
            </p>
          </aside>

          <div className="admin-content">
            {children}
          </div>
        </div>

        <footer>
          MiniBase ·{' '}
          {localSession
            ? 'Local administrator dashboard'
            : 'Access-protected administrator dashboard'}
        </footer>
      </div>
    </main>
  )
}
