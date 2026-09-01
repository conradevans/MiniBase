import { useCallback, useEffect, useState } from 'react'

import { toAdminDatabase } from '../api/admin'
import { safeErrorMessage } from '../api/request'
import AppLink from './AppLink'

export default function OverviewPage({ api, navigate }) {
  const [state, setState] = useState({
    loading: true,
    error: '',
    health: null,
    status: null,
    databases: [],
  })

  const loadOverview = useCallback(async () => {
    setState((current) => ({ ...current, loading: true, error: '' }))
    try {
      const [health, status, databases] = await Promise.all([
        api.getHealth(),
        api.getStatus(),
        api.getDatabases(),
      ])
      setState({
        loading: false,
        error: '',
        status,
        health,
        databases: databases.map(toAdminDatabase),
      })
    } catch (error) {
      setState({
        loading: false,
        error: safeErrorMessage(error, 'Unable to load the MiniBase overview.'),
        status: null,
        health: null,
        databases: [],
      })
    }
  }, [api])

  useEffect(() => {
    void loadOverview()
  }, [loadOverview])

  const counts = state.databases.reduce(
    (result, database) => ({
      ...result,
      [database.status]: (result[database.status] || 0) + 1,
    }),
    {},
  )

  return (
    <>
      <section className="page-hero">
        <div>
          <p className="eyebrow">ADMIN / OVERVIEW</p>
          <h1>PostgreSQL, under control.</h1>
          <p>
            Review the local control plane and provision isolated application
            databases from one focused workspace.
          </p>
        </div>
        <button
          className="button secondary"
          type="button"
          onClick={loadOverview}
          disabled={state.loading}
        >
          {state.loading ? 'Refreshing…' : 'Refresh'}
        </button>
      </section>

      {state.error ? <div className="notice error">{state.error}</div> : null}

      {state.loading && !state.status ? (
        <div className="empty-state">Loading MiniBase overview…</div>
      ) : (
        <>
          <section className="stats-grid" aria-label="Database summary">
            <Stat label="Total databases" value={state.databases.length} />
            <Stat label="Ready" value={counts.ready || 0} tone="ready" />
            <Stat label="Provisioning" value={counts.provisioning || 0} tone="provisioning" />
            <Stat label="Error" value={counts.error || 0} tone="error" />
          </section>

          <section className="section-card service-card">
            <div>
              <p className="eyebrow">CONTROL PLANE</p>
              <h2>Service status</h2>
            </div>
            <dl className="definition-grid">
              <div><dt>MiniBase</dt><dd>{state.health?.status === 'ok' ? 'Online' : 'Unavailable'}</dd></div>
              <div><dt>Metadata database</dt><dd>{state.status?.metadataDatabase || 'Unavailable'}</dd></div>
              <div><dt>API version</dt><dd>{state.status?.apiVersion || 'Unavailable'}</dd></div>
              <div><dt>Schema version</dt><dd>{state.status?.schemaVersion ?? 'Unavailable'}</dd></div>
            </dl>
            <AppLink className="text-link" href="/admin/databases" navigate={navigate}>
              Manage databases →
            </AppLink>
          </section>
        </>
      )}
    </>
  )
}

function Stat({ label, value, tone = '' }) {
  return <div className={`stat-card ${tone}`}><span>{label}</span><strong>{value}</strong></div>
}
