import { useCallback, useEffect, useState } from 'react'

import { toAdminBackup, toAdminDatabase } from '../api/admin'
import { safeErrorMessage } from '../api/request'
import { databaseDetailPath } from '../routing'
import AppLink from './AppLink'
import BackupList from './BackupList'
import RestoreBackupDialog from './RestoreBackupDialog'

export default function BackupsPage({ api, navigate }) {
  const [backups, setBackups] = useState([])
  const [databases, setDatabases] = useState([])
  const [selectedBackup, setSelectedBackup] = useState(null)
  const [restoredDatabase, setRestoredDatabase] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [backupResult, databaseResult] = await Promise.all([
        api.getBackups(),
        api.getDatabases(),
      ])
      setBackups(backupResult.map(toAdminBackup))
      setDatabases(databaseResult.map(toAdminDatabase))
      setError('')
    } catch (requestError) {
      setError(safeErrorMessage(requestError, 'Unable to load MiniBase backups.'))
    } finally {
      setLoading(false)
    }
  }, [api])

  useEffect(() => {
    void load()
  }, [load])

  async function restore(input) {
    const database = input.mode === 'new'
      ? await api.restoreBackupAsNew(selectedBackup.id, input.displayName)
      : await api.restoreBackupReplace(selectedBackup.id, input.targetDatabaseId)
    setRestoredDatabase(toAdminDatabase(database))
    setSelectedBackup(null)
    await load()
  }

  return (
    <>
      <section className="page-hero compact">
        <div>
          <p className="eyebrow">ADMIN / BACKUPS</p>
          <h1>Backups</h1>
          <p>Verified local PostgreSQL archives and deliberate restore workflows.</p>
        </div>
        <div className="page-actions">
          <button className="button secondary" type="button" onClick={load} disabled={loading}>
            {loading ? 'Refreshing…' : 'Refresh'}
          </button>
        </div>
      </section>

      <section className="policy-grid" aria-label="Backup policy">
        <article><span>AUTOMATIC POLICY</span><strong>Daily</strong></article>
        <article><span>RETENTION</span><strong>7 daily / 4 weekly</strong></article>
        <article><span>SCHEDULER ACTIVATION</span><strong>Pending deployment integration</strong></article>
      </section>

      {restoredDatabase ? (
        <div className="notice success">
          <span>{restoredDatabase.displayName} was restored successfully.</span>
          <AppLink href={databaseDetailPath(restoredDatabase.id)} navigate={navigate}>Open database →</AppLink>
        </div>
      ) : null}
      {error ? <div className="notice error">{error}</div> : null}

      <section className="content-section">
        <div className="section-heading">
          <div>
            <p className="eyebrow">LOCAL ARCHIVES</p>
            <h2>Backup inventory</h2>
          </div>
          <span className="record-count">{backups.length} total</span>
        </div>
        {loading && backups.length === 0 ? (
          <div className="empty-state">Loading backups…</div>
        ) : (
          <BackupList
            backups={backups}
            emptyMessage="No backups yet. Create a manual backup from a database detail page."
            onRestore={setSelectedBackup}
          />
        )}
      </section>

      <p className="local-backup-note">
        Backups remain on this Dell. They protect against logical and application errors, but not SSD or hardware failure.
      </p>

      {selectedBackup ? (
        <RestoreBackupDialog
          backup={selectedBackup}
          databases={databases}
          onClose={() => setSelectedBackup(null)}
          onRestored={restore}
        />
      ) : null}
    </>
  )
}
