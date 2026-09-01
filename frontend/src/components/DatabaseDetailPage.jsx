import { useEffect, useState } from 'react'

import { toAdminDatabase } from '../api/admin'
import { safeErrorMessage } from '../api/request'
import { formatTimestamp } from '../utils/format'
import AppLink from './AppLink'
import StatusBadge from './StatusBadge'

export default function DatabaseDetailPage({ api, databaseID, navigate }) {
  const [state, setState] = useState({ loading: true, error: '', database: null })

  useEffect(() => {
    let active = true
    async function loadDatabase() {
      try {
        const database = toAdminDatabase(await api.getDatabase(databaseID))
        if (active) {
          setState({ loading: false, error: '', database })
        }
      } catch (error) {
        if (active) {
          setState({
            loading: false,
            error: safeErrorMessage(error, 'Unable to load this database.'),
            database: null,
          })
        }
      }
    }
    void loadDatabase()
    return () => {
      active = false
    }
  }, [api, databaseID])

  if (state.loading) {
    return <div className="empty-state">Loading database…</div>
  }
  if (state.error || !state.database) {
    return (
      <section className="detail-error">
        <p className="eyebrow">DATABASE NOT AVAILABLE</p>
        <h1>Database could not be loaded.</h1>
        <p>{state.error || 'The requested database does not exist.'}</p>
        <AppLink className="button secondary" href="/admin/databases" navigate={navigate}>Back to databases</AppLink>
      </section>
    )
  }

  const database = state.database
  return (
    <>
      <AppLink className="back-link" href="/admin/databases" navigate={navigate}>← All databases</AppLink>
      <section className="page-hero detail-hero">
        <div>
          <p className="eyebrow">DATABASE / OVERVIEW</p>
          <h1>{database.displayName}</h1>
          <code>{database.internalName}</code>
        </div>
        <StatusBadge status={database.status} />
      </section>

      <nav className="detail-tabs" aria-label="Database sections">
        <span className="active">Overview</span>
        <span>Connection</span>
        <span className="unavailable">Backups · Later</span>
        <span className="unavailable">Activity · Later</span>
        <span className="unavailable">Settings · Later</span>
      </nav>

      <section className="detail-grid">
        <article className="section-card">
          <p className="eyebrow">RESOURCE METADATA</p>
          <h2>Overview</h2>
          <dl className="definition-grid detail-definitions">
            <div><dt>Display name</dt><dd>{database.displayName}</dd></div>
            <div><dt>Status</dt><dd>{database.status}</dd></div>
            <div><dt>Internal name</dt><dd><code>{database.internalName}</code></dd></div>
            <div><dt>Resource ID</dt><dd><code>{database.id}</code></dd></div>
            <div><dt>Created</dt><dd>{formatTimestamp(database.createdAt)}</dd></div>
            <div><dt>Updated</dt><dd>{formatTimestamp(database.updatedAt)}</dd></div>
          </dl>
        </article>

        <article className="section-card connection-card">
          <p className="eyebrow">CONNECTION</p>
          <h2>Managed securely</h2>
          <p>
            Connection credentials are managed securely by MiniBase and are not
            exposed in browser responses.
          </p>
          <p>
            Automatic application attachment arrives with future MiniDeploy
            integration.
          </p>
        </article>
      </section>

      <section className="future-grid">
        <article><span>BACKUPS</span><strong>Coming in Phase 5</strong><p>Backup and restore workflows are not available yet.</p></article>
        <article><span>ACTIVITY</span><strong>Coming later</strong><p>Provisioning activity history is not available yet.</p></article>
        <article><span>SETTINGS</span><strong>Coming later</strong><p>Deletion is intentionally unavailable until dependency and backup safety exists.</p></article>
      </section>
    </>
  )
}
