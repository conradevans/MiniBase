import AppLink from './AppLink'
import Brand from './Brand'

export default function AdminShell({ active, navigate, children }) {
  return (
    <main className="admin-page">
      <div className="app-shell">
        <header className="topbar">
          <Brand navigate={navigate} subtitle="Database control plane" />
          <div className="control-plane-state">
            <span className="status-dot status-ready" aria-hidden="true" />
            <span>
              <small>ACCESS</small>
              <strong>Local administrator</strong>
            </span>
          </div>
        </header>

        <div className="admin-grid">
          <aside className="sidebar">
            <nav aria-label="MiniBase navigation">
              <AppLink
                className={active === 'overview' ? 'nav-item active' : 'nav-item'}
                href="/admin"
                navigate={navigate}
              >
                Overview
              </AppLink>
              <AppLink
                className={active === 'databases' ? 'nav-item active' : 'nav-item'}
                href="/admin/databases"
                navigate={navigate}
              >
                Databases
              </AppLink>
              <span className="nav-item unavailable" aria-disabled="true">
                Backups <small>Later</small>
              </span>
              <span className="nav-item unavailable" aria-disabled="true">
                Activity <small>Later</small>
              </span>
            </nav>
            <p className="sidebar-note">
              Loopback-only. Public access and authentication arrive in a later
              security phase.
            </p>
          </aside>

          <div className="admin-content">{children}</div>
        </div>

        <footer>MiniBase · Local administrator dashboard</footer>
      </div>
    </main>
  )
}
