import { useCallback, useEffect, useState } from 'react'

import { safeErrorMessage } from '../api/request'
import ActivityList from './ActivityList'

export default function ActivityPage({ api }) {
  const [state, setState] = useState({
    loading: true,
    error: '',
    events: [],
  })

  const loadActivity = useCallback(async () => {
    setState((current) => ({
      ...current,
      loading: true,
      error: '',
    }))

    try {
      const events = await api.getActivity()
      setState({
        loading: false,
        error: '',
        events,
      })
    } catch (error) {
      setState({
        loading: false,
        error: safeErrorMessage(
          error,
          'Unable to load MiniBase activity.',
        ),
        events: [],
      })
    }
  }, [api])

  useEffect(() => {
    void loadActivity()
  }, [loadActivity])

  return (
    <>
      <section className="page-hero compact">
        <div>
          <p className="eyebrow">
            CONTROL PLANE / HISTORY
          </p>
          <h1>Activity</h1>
          <p>
            Recent database lifecycle events recorded by
            MiniBase.
          </p>
        </div>

        <div className="page-actions">
          <button
            className="button secondary"
            type="button"
            disabled={state.loading}
            onClick={loadActivity}
          >
            {state.loading
              ? 'Refreshing…'
              : 'Refresh'}
          </button>
        </div>
      </section>

      <section className="content-section">
        <div className="section-heading">
          <div>
            <p className="eyebrow">
              SAFE STRUCTURED EVENTS
            </p>
            <h2>Recent activity</h2>
          </div>

          <span className="record-count">
            {state.events.length} EVENTS
          </span>
        </div>

        {state.error ? (
          <div className="notice error" role="alert">
            {state.error}
          </div>
        ) : null}

        {state.loading && state.events.length === 0 ? (
          <div className="empty-state">
            Loading activity…
          </div>
        ) : (
          <ActivityList events={state.events} />
        )}
      </section>
    </>
  )
}
