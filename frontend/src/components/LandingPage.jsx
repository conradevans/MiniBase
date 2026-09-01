import AppLink from './AppLink'
import Brand from './Brand'

export default function LandingPage({ navigate }) {
  return (
    <main className="public-page">
      <div className="site-shell">
        <header className="public-nav">
          <Brand navigate={navigate} subtitle="PostgreSQL control plane" />
          <span className="local-badge">
            <span className="status-dot status-ready" aria-hidden="true" />
            Local control plane
          </span>
        </header>

        <section className="landing-hero">
          <div className="landing-copy">
            <p className="eyebrow">REACTORLAB / MINIBASE</p>
            <h1>Manage PostgreSQL without managing PostgreSQL.</h1>
            <p className="hero-copy">
              MiniBase provisions isolated databases and application roles on
              ReactorLab infrastructure while keeping operational credentials
              out of the dashboard.
            </p>
            <div className="hero-actions">
              <AppLink
                className="button primary"
                href="/admin"
                navigate={navigate}
              >
                Open Admin Dashboard
              </AppLink>
              <AppLink
                className="button secondary"
                href="/guest"
                navigate={navigate}
              >
                Guest View
              </AppLink>
            </div>
          </div>

          <aside className="principles-card">
            <p className="eyebrow">WHAT MINIBASE MANAGES</p>
            <ul>
              <li>
                <strong>Isolated by default</strong>
                <span>One dedicated role for every database.</span>
              </li>
              <li>
                <strong>Private infrastructure</strong>
                <span>PostgreSQL stays on an internal Docker network.</span>
              </li>
              <li>
                <strong>Secret-conscious</strong>
                <span>Credentials never appear in browser responses.</span>
              </li>
            </ul>
          </aside>
        </section>

        <aside className="boundary-note">
          <span aria-hidden="true">i</span>
          <p>
            Phase 4 is available only through localhost or SSH port forwarding.
            These route names are not an authentication boundary; do not route
            MiniBase publicly yet.
          </p>
        </aside>

        <footer>MiniBase · ReactorLab database control plane</footer>
      </div>
    </main>
  )
}
