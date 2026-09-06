import { formatTimestamp } from '../utils/format'

const typeLabels = {
  database_create: 'Database created',
  database_delete: 'Database deleted',
  backup_create: 'Backup created',
  backup_restore_new: 'Restore as new',
  backup_restore_replace: 'Database restored',
  attachment_attach: 'Deployment attached',
  attachment_detach: 'Deployment detached',
  automatic_backup: 'Automatic backup',
  retention_prune: 'Backup retention',
}

const sourceLabels = {
  admin: 'Admin',
  minideploy: 'MiniDeploy',
  system: 'System',
}

function outcomeLabel(value) {
  return value.charAt(0).toUpperCase() + value.slice(1)
}

export default function ActivityList({
  events,
  emptyMessage = 'No activity has been recorded yet.',
}) {
  if (events.length === 0) {
    return (
      <div className="empty-state">
        {emptyMessage}
      </div>
    )
  }

  return (
    <div
      className="activity-list"
      aria-label="Activity history"
    >
      {events.map((event, index) => (
        <article
          className="activity-row"
          key={[
            event.createdAt,
            event.type,
            event.databaseId,
            index,
          ].join(':')}
        >
          <div
            className={[
              'activity-indicator',
              `activity-${event.outcome}`,
            ].join(' ')}
            aria-hidden="true"
          />

          <div className="activity-main">
            <div className="activity-title">
              <strong>
                {typeLabels[event.type]}
              </strong>

              <span
                className={[
                  'activity-outcome',
                  `activity-${event.outcome}`,
                ].join(' ')}
              >
                {outcomeLabel(event.outcome)}
              </span>
            </div>

            <p>{event.detail}</p>

            {event.databaseDisplayName ? (
              <span className="activity-database">
                {event.databaseDisplayName}
              </span>
            ) : null}
          </div>

          <div className="activity-meta">
            <strong>
              {sourceLabels[event.source]}
            </strong>
            <span>
              {formatTimestamp(event.createdAt)}
            </span>
          </div>
        </article>
      ))}
    </div>
  )
}
