import { useCallback, useEffect, useRef, useState } from 'react'

import { toAdminBackup, toAdminDatabase } from '../api/admin'
import { safeErrorMessage } from '../api/request'
import { formatTimestamp } from '../utils/format'
import AppLink from './AppLink'
import BackupList from './BackupList'
import DeleteDatabaseDialog from './DeleteDatabaseDialog'
import RestoreBackupDialog from './RestoreBackupDialog'
import StatusBadge from './StatusBadge'

export default function DatabaseDetailPage({ api, databaseID, navigate }) {
  const [state, setState] = useState({ loading: true, error: '', database: null })
  const [backups, setBackups] = useState([])
  const [databases, setDatabases] = useState([])
  const [backupLoading, setBackupLoading] = useState(true)
  const [backupPending, setBackupPending] = useState(false)
  const [backupError, setBackupError] = useState('')
  const [backupNotice, setBackupNotice] = useState('')
  const [selectedBackup, setSelectedBackup] = useState(null)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const createBackupButtonRef = useRef(null)

  const loadBackups = useCallback(async () => {
    setBackupLoading(true)
    try {
      const result = await api.getDatabaseBackups(databaseID)
      setBackups(result.map(toAdminBackup))
      setBackupError('')
    } catch (error) {
      setBackupError(safeErrorMessage(error, 'Unable to load database backups.'))
    } finally {
      setBackupLoading(false)
    }
  }, [api, databaseID])

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

  useEffect(() => {
    void loadBackups()
  }, [loadBackups])

  useEffect(() => {
    let active = true
    api.getDatabases()
      .then((result) => {
        if (active) {
          setDatabases(result.map(toAdminDatabase))
        }
      })
      .catch(() => {
        if (active) {
          setDatabases([])
        }
      })
    return () => {
      active = false
    }
  }, [api])

  async function createBackup() {
    if (backupPending) {
      return
    }
    setBackupPending(true)
    setBackupError('')
    setBackupNotice('')
    try {
      await api.createBackup(databaseID)
      setBackupNotice('Verified manual backup created successfully.')
      await loadBackups()
    } catch (error) {
      setBackupError(safeErrorMessage(error, 'MiniBase could not create this backup.'))
    } finally {
      setBackupPending(false)
      window.setTimeout(() => createBackupButtonRef.current?.focus(), 0)
    }
  }

  async function restoreBackup(input) {
    const database = input.mode === 'new'
      ? await api.restoreBackupAsNew(selectedBackup.id, input.displayName)
      : await api.restoreBackupReplace(selectedBackup.id, input.targetDatabaseId)
    const restored = toAdminDatabase(database)
    setSelectedBackup(null)
    setBackupNotice(`${restored.displayName} was restored successfully.`)
    if (restored.id === databaseID) {
      setState({ loading: false, error: '', database: restored })
    }
    await loadBackups()
  }

  async function deleteDatabase() {
    await api.deleteDatabase(databaseID)
    setDeleteDialogOpen(false)
    navigate('/admin/databases')
  }

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
  const deletionAttachment = database.attachments[0] || null
  const deletionAllowed =
    !deletionAttachment &&
    (database.status === 'ready' || database.status === 'error')

  const attachment = database.attachments.find(
    (item) => item.consumerType === 'minideploy' && item.bindingName === 'primary',
  )
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
        <a href="#backups">Backups</a>
        <span className="unavailable">Activity · Later</span>
        <a href="#settings">Settings</a>
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
          {attachment ? (
            <div className="attachment-detail">
              <strong>Attached application</strong>
              <dl className="definition-grid">
                <div><dt>Application</dt><dd>{attachment.consumerRef}</dd></div>
                <div><dt>Service</dt><dd>MiniDeploy</dd></div>
                <div><dt>Binding</dt><dd>Primary</dd></div>
              </dl>
            </div>
          ) : (
            <p>No MiniDeploy application is attached to this database.</p>
          )}
        </article>
      </section>

      <section className="content-section database-backups" id="backups">
        <div className="section-heading">
          <div>
            <p className="eyebrow">VERIFIED LOCAL ARCHIVES</p>
            <h2>Backups</h2>
          </div>
          <div className="page-actions">
            <button className="button secondary" type="button" onClick={loadBackups} disabled={backupLoading || backupPending}>
              {backupLoading ? 'Refreshing…' : 'Refresh'}
            </button>
            <button
              className="button primary"
              type="button"
              onClick={createBackup}
              disabled={backupPending || database.status !== 'ready'}
              ref={createBackupButtonRef}
            >
              {backupPending ? 'Creating backup…' : 'Create Backup'}
            </button>
          </div>
        </div>
        {backupNotice ? <div className="notice success">{backupNotice}</div> : null}
        {backupError ? <div className="notice error">{backupError}</div> : null}
        {backupLoading && backups.length === 0 ? (
          <div className="empty-state">Loading backups…</div>
        ) : (
          <BackupList
            backups={backups}
            emptyMessage="No backups for this database yet."
            onRestore={setSelectedBackup}
          />
        )}
      </section>

      <section className="future-grid">
        <article>
          <span>ACTIVITY</span>
          <strong>Coming later</strong>
          <p>Provisioning activity history is not available yet.</p>
        </article>
      </section>

      <section
        className="content-section database-settings"
        id="settings"
      >
        <div className="section-heading">
          <div>
            <p className="eyebrow">DATABASE SETTINGS</p>
            <h2>Settings</h2>
          </div>
        </div>

        <article className="section-card danger-zone">
          <div className="danger-zone-heading">
            <div>
              <p className="eyebrow">DANGER ZONE</p>
              <h2>Delete database</h2>
            </div>
          </div>

          <p>
            Permanently remove this PostgreSQL database, its isolated role
            and credential, all MiniBase backups for it, and its MiniBase
            metadata.
          </p>

          {deletionAttachment ? (
            <div className="notice error settings-notice">
              <span>
                This database is attached to{' '}
                <strong>{deletionAttachment.consumerRef}</strong>. Detach it
                from MiniDeploy before deleting it.
              </span>
            </div>
          ) : null}

          {!deletionAttachment && !deletionAllowed ? (
            <div className="notice error settings-notice">
              <span>
                This database is not currently in a state that can be deleted.
              </span>
            </div>
          ) : null}

          <div className="danger-zone-actions">
            <div>
              <strong>Permanent deletion</strong>
              <p>This action cannot be undone.</p>
            </div>

            <button
              className="button danger"
              type="button"
              disabled={!deletionAllowed}
              onClick={() => setDeleteDialogOpen(true)}
            >
              Delete database
            </button>
          </div>
        </article>
      </section>

      {selectedBackup ? (
        <RestoreBackupDialog
          backup={selectedBackup}
          databases={databases}
          onClose={() => setSelectedBackup(null)}
          onRestored={restoreBackup}
        />
      ) : null}

      {deleteDialogOpen ? (
        <DeleteDatabaseDialog
          database={database}
          onClose={() => setDeleteDialogOpen(false)}
          onDelete={deleteDatabase}
        />
      ) : null}
    </>
  )
}
