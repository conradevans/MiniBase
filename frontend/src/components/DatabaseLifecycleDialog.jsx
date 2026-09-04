import { useEffect, useMemo, useRef, useState } from 'react'

import { safeErrorMessage } from '../api/request'

export default function DatabaseLifecycleDialog({
  mode,
  database,
  attachment,
  deployments = [],
  onClose,
  onAttach,
  onDetach,
}) {
  const [selectedApp, setSelectedApp] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const dialogRef = useRef(null)
  const firstControlRef = useRef(null)

  const eligibleDeployments = useMemo(
    () => deployments.filter((deployment) => (
      deployment.supported &&
      !deployment.databaseAttached &&
      (
        deployment.databaseDetached ||
        deployment.status === 'running'
      )
    )),
    [deployments],
  )

  useEffect(() => {
    firstControlRef.current?.focus()

    function handleKeyDown(event) {
      if (event.key === 'Escape' && !pending) {
        onClose()
      }

      if (event.key !== 'Tab') {
        return
      }

      const focusable = dialogRef.current?.querySelectorAll(
        'button:not(:disabled), select:not(:disabled), input:not(:disabled), [href]',
      )

      if (!focusable?.length) {
        return
      }

      const first = focusable[0]
      const last = focusable[focusable.length - 1]

      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose, pending])

  async function handleSubmit(event) {
    event.preventDefault()

    if (pending) {
      return
    }

    if (mode === 'attach' && !selectedApp) {
      setError('Choose a MiniDeploy deployment first.')
      return
    }

    setPending(true)
    setError('')

    try {
      if (mode === 'attach') {
        await onAttach(selectedApp)
      } else {
        await onDetach()
      }
    } catch (requestError) {
      setError(
        safeErrorMessage(
          requestError,
          mode === 'attach'
            ? 'MiniBase could not attach this database.'
            : 'MiniBase could not detach this database.',
        ),
      )
      setPending(false)
    }
  }

  const attaching = mode === 'attach'
  const title = attaching ? 'Attach database' : 'Detach database'

  return (
    <div
      className="modal-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !pending) {
          onClose()
        }
      }}
    >
      <section
        aria-labelledby="database-lifecycle-title"
        aria-modal="true"
        className="modal lifecycle-dialog"
        ref={dialogRef}
        role="dialog"
      >
        <div className="modal-header">
          <div>
            <p className="eyebrow">MINIDEPLOY CONNECTION</p>
            <h2 id="database-lifecycle-title">{title}</h2>
          </div>

          <button
            aria-label={`Close ${title.toLowerCase()} dialog`}
            className="icon-button"
            type="button"
            onClick={onClose}
            disabled={pending}
          >
            ×
          </button>
        </div>

        <form className="modal-content" onSubmit={handleSubmit}>
          {attaching ? (
            <>
              <div className="lifecycle-note">
                <strong>Connect {database.displayName} to a deployment.</strong>
                <p>
                  MiniDeploy will securely provide DATABASE_URL and either
                  redeploy the running app or resume a deployment that was
                  stopped after its database was detached.
                </p>
              </div>

              {eligibleDeployments.length ? (
                <label className="field" htmlFor="deployment-select">
                  <span>MiniDeploy deployment</span>
                  <select
                    id="deployment-select"
                    ref={firstControlRef}
                    value={selectedApp}
                    disabled={pending}
                    onChange={(event) => {
                      setSelectedApp(event.target.value)
                      setError('')
                    }}
                  >
                    <option value="">Choose a deployment</option>

                    {eligibleDeployments.map((deployment) => (
                      <option
                        key={deployment.app}
                        value={deployment.app}
                      >
                        {deployment.app}
                        {' — '}
                        {deployment.databaseDetached
                          ? 'stopped, database detached'
                          : 'running'}
                      </option>
                    ))}
                  </select>
                </label>
              ) : (
                <div
                  className="notice error lifecycle-empty"
                  ref={firstControlRef}
                  tabIndex="-1"
                >
                  No compatible MiniDeploy deployments are available.
                </div>
              )}
            </>
          ) : (
            <div
              className="lifecycle-note"
              ref={firstControlRef}
              tabIndex="-1"
            >
              <strong>
                Detach {database.displayName} from {attachment?.consumerRef}.
              </strong>
              <p>
                MiniDeploy will stop that deployment before releasing the
                database connection. The database, schema, data, role,
                credential, and MiniBase backups stay intact.
              </p>
              <p>
                You can attach this database again later and MiniDeploy will
                restart the deployment after its health checks pass.
              </p>
            </div>
          )}

          {error ? (
            <div className="notice error" role="alert">
              {error}
            </div>
          ) : null}

          <div className="dialog-actions">
            <button
              className="button secondary"
              type="button"
              onClick={onClose}
              disabled={pending}
            >
              Cancel
            </button>

            <button
              className={attaching ? 'button primary' : 'button secondary'}
              type="submit"
              disabled={
                pending ||
                (attaching && (
                  !selectedApp ||
                  eligibleDeployments.length === 0
                ))
              }
            >
              {pending
                ? (
                  attaching
                    ? 'Attaching database…'
                    : 'Detaching database…'
                )
                : (
                  attaching
                    ? 'Attach database'
                    : 'Detach database'
                )}
            </button>
          </div>
        </form>
      </section>
    </div>
  )
}
