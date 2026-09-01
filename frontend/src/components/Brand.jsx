import AppLink from './AppLink'

export default function Brand({ navigate, subtitle = 'PostgreSQL control plane' }) {
  return (
    <AppLink
      aria-label="MiniBase home"
      className="brand brand-link"
      href="/"
      navigate={navigate}
    >
      <span className="brand-mark" aria-hidden="true">
        B
      </span>
      <span>
        <strong className="brand-name">MiniBase</strong>
        <span className="brand-subtitle">ReactorLab · {subtitle}</span>
      </span>
    </AppLink>
  )
}
