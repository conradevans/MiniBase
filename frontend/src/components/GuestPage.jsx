import { useCallback, useEffect, useState } from 'react'

import { toGuestDatabase } from '../api/guest'
import { safeErrorMessage } from '../api/request'
import GlobalHeader from './GlobalHeader'
import StatusBadge from './StatusBadge'

export default function GuestPage({ api, navigate }) {
  const [state, setState] = useState({
    loading: true,
    error: '',
    summary: { total: 0, showing: 0, hidden: 0 },
    databases: [],
  })

  const loadGuestData = useCallback(async () => {
    setState((current) => ({ ...current, loading: true, error: '' }))
    try {
      const [, result] = await Promise.all([api.getStatus(), api.getDatabases()])
      setState({
        loading: false,
        error: '',
        summary: result.summary,
        databases: result.databases.map(toGuestDatabase),
      })
    } catch (error) {
      setState({
        loading: false,
        error: safeErrorMessage(error, 'Unable to load the Guest view.'),
        summary: { total: 0, showing: 0, hidden: 0 },
        databases: [],
      })
    }
  }, [api])

  useEffect(() => {
    void loadGuestData()
  }, [loadGuestData])

  return (
    <main className="guest-page">
      <div className="site-shell">
        <GlobalHeader
          mode="guest"
          navigate={navigate}
          sessionLabel="Guest View"
        />

        <section className="guest-hero">
          <div>
            <p className="eyebrow">GUEST / READ ONLY</p>
            <h1>See what MiniBase manages.</h1>
            <p>
              Guest View exposes database names and lifecycle states only.
              Internal identifiers, connection details, and credentials remain
              on the server.
            </p>
          </div>
          <div className="guest-summary" aria-label="Guest database summary">
            <div><span>TOTAL</span><strong>{state.summary.total}</strong></div>
            <div><span>SHOWING</span><strong>{state.summary.showing}</strong></div>
            <div><span>HIDDEN</span><strong>{state.summary.hidden}</strong></div>
          </div>
        </section>

        <section className="content-section">
          <div className="section-heading">
            <div>
              <p className="eyebrow">SAFE METADATA</p>
              <h2>Databases</h2>
            </div>
            <button
              className="button secondary"
              type="button"
              onClick={loadGuestData}
              disabled={state.loading}
            >
              {state.loading ? 'Refreshing…' : 'Refresh'}
            </button>
          </div>

          {state.error ? <div className="notice error">{state.error}</div> : null}
          {state.loading && state.databases.length === 0 ? (
            <div className="empty-state">Loading Guest View…</div>
          ) : state.databases.length === 0 ? (
            <div className="empty-state">No databases are currently shared in Guest View.</div>
          ) : (
            <div className="guest-database-grid">
              {state.databases.map((database) => (
                <article className="guest-database-card" key={database.id}>
                  <h3>{database.displayName}</h3>
                  <StatusBadge status={database.status} />
                </article>
              ))}
            </div>
          )}
        </section>

        <aside className="boundary-note">
          <span aria-hidden="true">i</span>
          <p>Guest endpoints are read-only and use a dedicated safe response format.</p>
        </aside>
        <footer>MiniBase · Read-only Guest View</footer>
      </div>
    </main>
  )
}
