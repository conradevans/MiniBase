import { formatBytes, formatTimestamp, shortResourceID } from '../utils/format'
import StatusBadge from './StatusBadge'

const kindLabels = {
  manual: 'Manual',
  automatic: 'Automatic',
  pre_restore: 'Pre-restore safety',
}

export default function BackupList({ backups, emptyMessage, onRestore }) {
  if (backups.length === 0) {
    return <div className="empty-state">{emptyMessage}</div>
  }

  return (
    <div className="backup-list">
      {backups.map((backup) => (
        <article className="backup-row" key={backup.id}>
          <div className="backup-primary">
            <strong>{backup.databaseDisplayName}</strong>
            <code title={backup.id}>{shortResourceID(backup.id)}</code>
          </div>
          <div className="database-meta">
            <span>KIND</span>
            <strong>{kindLabels[backup.kind] || backup.kind}</strong>
          </div>
          <StatusBadge status={backup.status} />
          <div className="database-meta">
            <span>SIZE</span>
            <strong>{backup.status === 'ready' ? formatBytes(backup.sizeBytes) : '—'}</strong>
          </div>
          <div className="database-meta">
            <span>CREATED</span>
            <strong>{formatTimestamp(backup.createdAt)}</strong>
          </div>
          {onRestore ? (
            <button
              className="button secondary compact-button"
              type="button"
              disabled={backup.status !== 'ready'}
              onClick={() => onRestore(backup)}
            >
              Restore
            </button>
          ) : null}
        </article>
      ))}
    </div>
  )
}
