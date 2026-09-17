import { useCallback, useEffect, useState } from 'react'

import { toAdminDatabase } from '../api/admin'
import { safeErrorMessage } from '../api/request'

export default function VisibilityPage({ api }) {
  const [databases, setDatabases] = useState([])
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState({})
  const [error, setError] = useState('')

  const loadDatabases = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.getDatabases()
      setDatabases(result.map(toAdminDatabase))
      setError('')
    } catch (requestError) {
      setError(
        safeErrorMessage(requestError, 'Unable to load database visibility.'),
      )
    } finally {
      setLoading(false)
    }
  }, [api])

  useEffect(() => {
    void loadDatabases()
  }, [loadDatabases])

  async function updateVisibility(database, guestVisible) {
    if (pending[database.id]) {
      return
    }

    const previousValue = database.guestVisible
    setError('')
    setPending((current) => ({ ...current, [database.id]: true }))
    setDatabases((current) => current.map((item) => (
      item.id === database.id
        ? { ...item, guestVisible }
        : item
    )))

    try {
      const result = await api.updateGuestVisibility(database.id, guestVisible)
      setDatabases((current) => current.map((item) => (
        item.id === result.id
          ? { ...item, guestVisible: result.guestVisible }
          : item
      )))
    } catch (requestError) {
      setDatabases((current) => current.map((item) => (
        item.id === database.id
          ? { ...item, guestVisible: previousValue }
          : item
      )))
      setError(
        safeErrorMessage(requestError, 'Unable to update Guest View visibility.'),
      )
    } finally {
      setPending((current) => {
        const next = { ...current }
        delete next[database.id]
        return next
      })
    }
  }

  const visibleCount = databases.filter((database) => database.guestVisible).length
  const hiddenCount = databases.length - visibleCount

  return (
    <>
      {error ? (
        <div className="notice error visibility-feedback">{error}</div>
      ) : null}

      <section className="visibility-page">
        <div className="visibility-page-heading">
          <p className="eyebrow">ADMIN / VISIBILITY</p>
          <h1>Visibility</h1>
          <p className="hero-copy">
            Control which databases are listed in Guest View. Hidden databases
            remain active and continue serving attached applications.
          </p>
        </div>

        <div
          className="visibility-summary"
          aria-label="Guest View visibility summary"
        >
          <div><span>TOTAL</span><strong>{databases.length}</strong></div>
          <div><span>VISIBLE</span><strong>{visibleCount}</strong></div>
          <div><span>HIDDEN</span><strong>{hiddenCount}</strong></div>
        </div>

        {loading && databases.length === 0 ? (
          <div className="empty-state">Loading databases…</div>
        ) : databases.length === 0 ? (
          <div className="empty-state">No databases yet.</div>
        ) : (
          <div className="visibility-list">
            {databases.map((database) => {
              const isPending = Boolean(pending[database.id])
              const isVisible = Boolean(database.guestVisible)

              return (
                <div className="visibility-row" key={database.id}>
                  <div className="visibility-copy">
                    <strong>{database.displayName}</strong>
                    <span className={isVisible ? 'visible' : 'hidden'}>
                      {isPending
                        ? 'Saving…'
                        : isVisible
                          ? 'Visible in Guest View'
                          : 'Hidden from Guest View'}
                    </span>
                  </div>

                  <button
                    type="button"
                    role="switch"
                    aria-checked={isVisible}
                    aria-label={`List ${database.displayName} in Guest View`}
                    className={
                      isVisible
                        ? 'visibility-switch visible'
                        : 'visibility-switch hidden'
                    }
                    disabled={isPending}
                    onClick={() => {
                      void updateVisibility(database, !isVisible)
                    }}
                  >
                    <span className="visibility-switch-thumb" />
                  </button>
                </div>
              )
            })}
          </div>
        )}
      </section>
    </>
  )
}
