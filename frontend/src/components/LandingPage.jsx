import AIIntegration from './AIIntegration'
import AppLink from './AppLink'
import GlobalHeader from './GlobalHeader'

export default function LandingPage({ navigate }) {
  return (
    <main className="public-page">
      <div className="site-shell">
        <GlobalHeader mode="root" navigate={navigate} />

        <section className="landing-hero minibase-access-hero">
          <div className="landing-copy">
            <p className="eyebrow">MINIBASE</p>
            <h1>Managed PostgreSQL for applications running in ReactorLab.</h1>
            <p className="hero-copy">
              MiniBase provisions isolated application databases and roles on
              the Dell while keeping PostgreSQL private and operational
              credentials out of the browser.
            </p>

            <dl className="access-explanation">
              <div>
                <dt>What it does</dt>
                <dd>Provides managed PostgreSQL databases for applications running through ReactorLab.</dd>
              </div>
              <div>
                <dt>How it works</dt>
                <dd>Provisions isolated databases and roles, integrates with MiniDeploy, and manages backups and database lifecycle operations through its control plane.</dd>
              </div>
              <div>
                <dt>Why it is useful</dt>
                <dd>Applications get one consistent managed database path instead of separately provisioning, exposing, and maintaining their own database service.</dd>
              </div>
            </dl>
          </div>

          <aside className="principles-card access-card">
            <p className="eyebrow">CHOOSE ACCESS</p>
            <h2>Open MiniBase</h2>
            <p>
              Administrator access opens database, backup, and lifecycle
              controls. Guest access provides a restricted, read-only view of
              database availability.
            </p>
            <div className="access-actions">
              <AppLink
                className="button primary"
                href="/admin"
                navigate={navigate}
              >
                Open Administrator
              </AppLink>
              <AppLink
                className="button secondary"
                href="/guest"
                navigate={navigate}
              >
                Guest Overview
              </AppLink>
            </div>
          </aside>
        </section>

        <AIIntegration />

        <aside className="boundary-note">
          <span aria-hidden="true">i</span>
          <p>
            PostgreSQL stays private. MiniDeploy supplies a managed connection
            only to supported attached applications; credentials are never
            shown on this page.
          </p>
        </aside>

        <footer>MiniBase · ReactorLab database control plane</footer>
      </div>
    </main>
  )
}
