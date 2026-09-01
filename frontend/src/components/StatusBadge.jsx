const labels = {
  metadata_only: 'Metadata only',
  provisioning: 'Provisioning',
  ready: 'Ready',
  error: 'Error',
}

export default function StatusBadge({ status }) {
  return (
    <span className={`status-badge status-${status}`}>
      <span className="status-dot" aria-hidden="true" />
      {labels[status] || status}
    </span>
  )
}
