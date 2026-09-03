import { useEffect, useRef, useState } from 'react'

import { safeErrorMessage } from '../api/request'

export default function DeleteDatabaseDialog({
  database,
  onClose,
  onDelete,
}) {
  const [confirmation, setConfirmation] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const dialogRef = useRef(null)
  const inputRef = useRef(null)

  const confirmed = confirmation === database.displayName

  useEffect(() => {
    inputRef.current?.focus()

    function handleKeyDown(event) {
      if (event.key === 'Escape' && !pending) {
        onClose()
      }

      if (event.key === 'Tab') {
        const focusable = dialogRef.current?.querySelectorAll(
          'button:not(:disabled), input:not(:disabled), [href]',
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
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose, pending])

  async function handleSubmit(event) {
    event.preventDefault()

    if (pending) {
      return
    }

    if (!confirmed) {
      setError(`Type ${database.displayName} exactly to confirm deletion.`)
      return
    }

    setPending(true)
    setError('')

    try {
      await onDelete()
    } catch (requestError) {
      setError(
        safeErrorMessage(
          requestError,
          'MiniBase could not delete the database.',
        ),
      )
      setPending(false)
    }
  }

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
        aria-labelledby="delete-database-title"
        aria-modal="true"
        className="modal delete-dialog"
        ref={dialogRef}
        role="dialog"
      >
        <div className="modal-header">
          <div>
            <p className="eyebrow">DESTRUCTIVE ACTION</p>
            <h2 id="delete-database-title">Delete database</h2>
          </div>

          <button
            aria-label="Close delete database dialog"
            className="icon-button"
            type="button"
            onClick={onClose}
            disabled={pending}
          >
            ×
          </button>
        </div>

        <form
          className="modal-content delete-form"
          onSubmit={handleSubmit}
        >
          <div className="destructive-warning">
            <strong>This permanently deletes {database.displayName}.</strong>
            <p>This action cannot be undone. MiniBase will remove:</p>
            <ul className="danger-list">
              <li>The PostgreSQL database</li>
              <li>The isolated database role and credential</li>
              <li>All MiniBase backups for this database</li>
              <li>The MiniBase database metadata</li>
            </ul>
          </div>

          <label className="field" htmlFor="delete-database-confirmation">
            <span>Type {database.displayName} to confirm</span>
            <input
              autoComplete="off"
              id="delete-database-confirmation"
              onChange={(event) => {
                setConfirmation(event.target.value)
                setError('')
              }}
              ref={inputRef}
              spellCheck="false"
              value={confirmation}
              disabled={pending}
            />
          </label>

          <p className="field-hint">
            The name must match exactly before deletion is enabled.
          </p>

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
              className="button danger"
              type="submit"
              disabled={pending || !confirmed}
            >
              {pending ? 'Deleting database…' : 'Delete database'}
            </button>
          </div>
        </form>
      </section>
    </div>
  )
}
