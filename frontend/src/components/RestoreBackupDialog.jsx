import { useEffect, useMemo, useRef, useState } from 'react'

import { safeErrorMessage } from '../api/request'
import { shortResourceID } from '../utils/format'

const maxDisplayNameLength = 200

export default function RestoreBackupDialog({
  backup,
  databases,
  onClose,
  onRestored,
}) {
  const [mode, setMode] = useState('new')
  const [displayName, setDisplayName] = useState('')
  const [targetID, setTargetID] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const dialogRef = useRef(null)
  const nameInputRef = useRef(null)

  const readyDatabases = useMemo(
    () => databases.filter((database) => database.status === 'ready'),
    [databases],
  )
  const target = readyDatabases.find((database) => database.id === targetID)

  useEffect(() => {
    nameInputRef.current?.focus()
    function handleKeyDown(event) {
      if (event.key === 'Escape' && !pending) {
        onClose()
      }
      if (event.key === 'Tab') {
        const focusable = dialogRef.current?.querySelectorAll(
          'button:not(:disabled), input:not(:disabled), select:not(:disabled)',
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

  function changeMode(nextMode) {
    setMode(nextMode)
    setError('')
    setConfirmation('')
    if (nextMode === 'replace' && !targetID && readyDatabases.length > 0) {
      setTargetID(readyDatabases[0].id)
    }
  }

  async function handleSubmit(event) {
    event.preventDefault()
    if (pending) {
      return
    }

    if (mode === 'new') {
      const normalized = displayName.trim()
      if (!normalized) {
        setError('A new display name is required.')
        return
      }
      if ([...normalized].length > maxDisplayNameLength) {
        setError(`Display name must be ${maxDisplayNameLength} characters or fewer.`)
        return
      }
      setPending(true)
      setError('')
      try {
        await onRestored({ mode: 'new', displayName: normalized })
      } catch (requestError) {
        setError(safeErrorMessage(requestError, 'MiniBase could not restore this backup.'))
        setPending(false)
      }
      return
    }

    if (!target) {
      setError('Select an available target database.')
      return
    }
    if (confirmation !== target.displayName) {
      setError(`Type “${target.displayName}” exactly to confirm replacement.`)
      return
    }
    setPending(true)
    setError('')
    try {
      await onRestored({ mode: 'replace', targetDatabaseId: target.id })
    } catch (requestError) {
      setError(safeErrorMessage(requestError, 'MiniBase could not replace the target database.'))
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
        aria-labelledby="restore-backup-title"
        aria-modal="true"
        className="modal restore-dialog"
        ref={dialogRef}
        role="dialog"
      >
        <div className="modal-header">
          <div>
            <p className="eyebrow">RESTORE / {shortResourceID(backup.id)}</p>
            <h2 id="restore-backup-title">Restore backup</h2>
          </div>
          <button
            aria-label="Close restore dialog"
            className="icon-button"
            type="button"
            onClick={onClose}
            disabled={pending}
          >
            ×
          </button>
        </div>

        <form className="modal-content restore-form" onSubmit={handleSubmit}>
          <fieldset className="restore-options" disabled={pending}>
            <legend>Restore mode</legend>
            <label className={mode === 'new' ? 'restore-option selected' : 'restore-option'}>
              <input
                aria-label="Restore as a new database"
                checked={mode === 'new'}
                name="restore-mode"
                onChange={() => changeMode('new')}
                type="radio"
              />
              <span>
                <strong>Restore as a new database</strong>
                <small>Safer default. The source and existing databases remain unchanged.</small>
              </span>
            </label>
            <label className={mode === 'replace' ? 'restore-option destructive selected' : 'restore-option destructive'}>
              <input
                aria-label="Replace an existing database"
                checked={mode === 'replace'}
                name="restore-mode"
                onChange={() => changeMode('replace')}
                type="radio"
              />
              <span>
                <strong>Replace an existing database</strong>
                <small>Destructive. Current contents are replaced after a verified safety backup.</small>
              </span>
            </label>
          </fieldset>

          {mode === 'new' ? (
            <label className="field" htmlFor="restore-display-name">
              <span>New database display name</span>
              <input
                id="restore-display-name"
                maxLength={maxDisplayNameLength}
                onChange={(event) => setDisplayName(event.target.value)}
                ref={nameInputRef}
                value={displayName}
                disabled={pending}
              />
            </label>
          ) : (
            <div className="replace-fields">
              <label className="field" htmlFor="restore-target">
                <span>Target database</span>
                <select
                  id="restore-target"
                  onChange={(event) => {
                    setTargetID(event.target.value)
                    setConfirmation('')
                  }}
                  value={targetID}
                  disabled={pending || readyDatabases.length === 0}
                >
                  {readyDatabases.length === 0 ? <option value="">No ready databases</option> : null}
                  {readyDatabases.map((database) => (
                    <option key={database.id} value={database.id}>
                      {database.displayName}
                    </option>
                  ))}
                </select>
              </label>
              {target ? (
                <>
                  <div className="destructive-warning" role="note">
                    <strong>{target.displayName}</strong> will be replaced. MiniBase will first create and retain a verified pre-restore safety backup.
                  </div>
                  <label className="field" htmlFor="restore-confirmation">
                    <span>Type {target.displayName} to confirm</span>
                    <input
                      id="restore-confirmation"
                      onChange={(event) => setConfirmation(event.target.value)}
                      value={confirmation}
                      disabled={pending}
                      autoComplete="off"
                    />
                  </label>
                </>
              ) : null}
            </div>
          )}

          {error ? <div className="notice error" role="alert">{error}</div> : null}
          <div className="dialog-actions">
            <button className="button secondary" type="button" onClick={onClose} disabled={pending}>Cancel</button>
            <button className={mode === 'replace' ? 'button danger' : 'button primary'} type="submit" disabled={pending}>
              {pending
                ? 'Restoring…'
                : mode === 'new'
                  ? 'Restore as new database'
                  : 'Replace database'}
            </button>
          </div>
        </form>
      </section>
    </div>
  )
}
