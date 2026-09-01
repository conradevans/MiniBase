import { useEffect, useRef, useState } from 'react'

import { safeErrorMessage } from '../api/request'

const maxDisplayNameLength = 200

export default function CreateDatabaseDialog({ onClose, onCreate }) {
  const [displayName, setDisplayName] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const dialogRef = useRef(null)
  const inputRef = useRef(null)

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
    const normalizedName = displayName.trim()
    if (!normalizedName) {
      setError('Display name is required.')
      return
    }
    if ([...normalizedName].length > maxDisplayNameLength) {
      setError(`Display name must be ${maxDisplayNameLength} characters or fewer.`)
      return
    }

    setPending(true)
    setError('')
    try {
      await onCreate(normalizedName)
    } catch (requestError) {
      setError(
        safeErrorMessage(requestError, 'MiniBase could not create the database.'),
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
        aria-labelledby="create-database-title"
        aria-modal="true"
        className="modal create-dialog"
        ref={dialogRef}
        role="dialog"
      >
        <div className="modal-header">
          <div>
            <p className="eyebrow">NEW RESOURCE</p>
            <h2 id="create-database-title">Create database</h2>
          </div>
          <button
            aria-label="Close create database dialog"
            className="icon-button"
            type="button"
            onClick={onClose}
            disabled={pending}
          >
            ×
          </button>
        </div>

        <form className="modal-content create-form" onSubmit={handleSubmit}>
          <p>
            MiniBase generates the database name, isolated application role,
            and credential securely.
          </p>
          <label className="field" htmlFor="database-display-name">
            <span>Display name</span>
            <input
              id="database-display-name"
              maxLength={maxDisplayNameLength}
              onChange={(event) => setDisplayName(event.target.value)}
              placeholder="MyScheduler Production"
              ref={inputRef}
              value={displayName}
              disabled={pending}
            />
          </label>
          <p className="field-hint">1–200 characters. You can use a friendly name.</p>
          {error ? <div className="notice error" role="alert">{error}</div> : null}
          <div className="dialog-actions">
            <button className="button secondary" type="button" onClick={onClose} disabled={pending}>Cancel</button>
            <button className="button primary" type="submit" disabled={pending}>
              {pending ? 'Creating database…' : 'Create database'}
            </button>
          </div>
        </form>
      </section>
    </div>
  )
}
