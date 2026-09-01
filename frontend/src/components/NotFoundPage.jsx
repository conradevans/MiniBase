import AppLink from './AppLink'
import Brand from './Brand'

export default function NotFoundPage({ navigate }) {
  return (
    <main className="public-page">
      <div className="site-shell">
        <header className="public-nav"><Brand navigate={navigate} /></header>
        <section className="detail-error standalone">
          <p className="eyebrow">404 / NOT FOUND</p>
          <h1>This MiniBase page does not exist.</h1>
          <AppLink className="button secondary" href="/" navigate={navigate}>Return home</AppLink>
        </section>
      </div>
    </main>
  )
}
