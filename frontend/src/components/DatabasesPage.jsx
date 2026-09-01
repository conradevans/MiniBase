import { useCallback, useEffect, useRef, useState } from 'react'

import { toAdminDatabase } from '../api/admin'
import { safeErrorMessage } from '../api/request'
import { databaseDetailPath } from '../routing'
import { formatTimestamp, shortResourceID } from '../utils/format'
import AppLink from './AppLink'
import CreateDatabaseDialog from './CreateDatabaseDialog'
import StatusBadge from './StatusBadge'

export default function DatabasesPage({ api, navigate }) {
  const [databases, setDatabases] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [createdDatabase, setCreatedDatabase] = useState(null)
  const createButtonRef = useRef(null)

  const loadDatabases = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.getDatabases()
      setDatabases(result.map(toAdminDatabase))
      setError('')
    } catch (requestError) {
      setError(
        safeErrorMessage(requestError, 'Unable to load MiniBase databases.'),
      )
    } finally {
      setLoading(false)
    }
  }, [api])

  useEffect(() => {
    void loadDatabases()
  }, [loadDatabases])

  function closeDialog() {
    setDialogOpen(false)
    window.setTimeout(() => createButtonRef.current?.focus(), 0)
  }

  async function createDatabase(displayName) {
    const result = toAdminDatabase(await api.createDatabase(displayName))
    setCreatedDatabase(result)
    setDialogOpen(false)
    window.setTimeout(() => createButtonRef.current?.focus(), 0)
    await loadDatabases()
  }

  return (
    <>
      <section className="page-hero compact">
        <div>
          <p className="eyebrow">ADMIN / DATABASES</p>
          <h1>Databases</h1>
          <p>Create and inspect isolated PostgreSQL resources managed by MiniBase.</p>
        </div>
        <div className="page-actions">
          <button className="button secondary" type="button" onClick={loadDatabases} disabled={loading}>
            {loading ? 'Refreshing…' : 'Refresh'}
          </button>
          <button
            className="button primary"
            type="button"
            onClick={() => {
              setCreatedDatabase(null)
              setDialogOpen(true)
            }}
            ref={createButtonRef}
          >
            Create Database
          </button>
        </div>
      </section>

      {createdDatabase ? (
        <div className="notice success">
          <span>{createdDatabase.displayName} was created successfully.</span>
          <AppLink href={databaseDetailPath(createdDatabase.id)} navigate={navigate}>Open database →</AppLink>
        </div>
      ) : null}
      {error ? <div className="notice error">{error}</div> : null}

      <section className="content-section">
        <div className="section-heading">
          <div>
            <p className="eyebrow">POSTGRESQL RESOURCES</p>
            <h2>Managed databases</h2>
          </div>
          <span className="record-count">{databases.length} total</span>
        </div>

        {loading && databases.length === 0 ? (
          <div className="empty-state">Loading databases…</div>
        ) : databases.length === 0 ? (
          <div className="empty-state empty-action">
            <h3>No databases yet.</h3>
            <p>Create the first isolated PostgreSQL database for a ReactorLab application.</p>
            <button className="button primary" type="button" onClick={() => setDialogOpen(true)}>Create Database</button>
          </div>
        ) : (
          <div className="database-list">
            {databases.map((database) => (
              <AppLink
                className="database-row"
                href={databaseDetailPath(database.id)}
                key={database.id}
                navigate={navigate}
              >
                <div className="database-primary">
                  <strong>{database.displayName}</strong>
                  <code>{database.internalName}</code>
                </div>
                <StatusBadge status={database.status} />
                <div className="database-meta"><span>CREATED</span><strong>{formatTimestamp(database.createdAt)}</strong></div>
                <div className="database-meta"><span>RESOURCE</span><strong>{shortResourceID(database.id)}</strong></div>
                <span className="row-arrow" aria-hidden="true">→</span>
              </AppLink>
            ))}
          </div>
        )}
      </section>

      {dialogOpen ? (
        <CreateDatabaseDialog onClose={closeDialog} onCreate={createDatabase} />
      ) : null}
    </>
  )
}
