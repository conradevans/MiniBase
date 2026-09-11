import { useEffect, useState } from 'react'

import AppLink from './AppLink'
import GlobalHeader from './GlobalHeader'

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
        <GlobalHeader
          mode="admin"
          navigate={navigate}
          sessionLabel={
            localSession || !session?.email
              ? 'Admin'
              : 'Admin · ' + session.email
          }
        />

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

              <AppLink
                className={
                  active === 'activity'
                    ? 'nav-item active'
                    : 'nav-item'
                }
                href="/admin/activity"
                navigate={navigate}
              >
                Activity
              </AppLink>
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
