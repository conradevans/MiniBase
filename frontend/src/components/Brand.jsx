import AppLink from './AppLink'

export default function Brand({ navigate }) {
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
      <strong className="brand-name">MiniBase</strong>
    </AppLink>
  )
}
