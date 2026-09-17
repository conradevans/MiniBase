import { useEffect, useMemo, useState } from 'react'

import { safeErrorMessage } from '../api/request'
import { routes } from '../routing'
import AppLink from './AppLink'
import StatusBadge from './StatusBadge'

const pageSizes = [25, 50, 100]

function objectLabel(type) {
  switch (type) {
    case 'table':
      return 'TABLE'
    case 'view':
      return 'VIEW'
    case 'materialized_view':
      return 'MATERIALIZED VIEW'
    default:
      return type
  }
}

function firstObject(catalog) {
  for (const schema of catalog.schemas) {
    if (schema.objects.length > 0) {
      return { schema: schema.name, ...schema.objects[0] }
    }
  }
  return null
}

function objectKey(object) {
  return object ? `${object.schema}\u0000${object.name}` : ''
}

function ExplorerCell({ cell }) {
  if (cell.kind === 'null') {
    return <span className="explorer-null">NULL</span>
  }
  const title = !cell.truncated && cell.value.length <= 2048
    ? cell.value
    : undefined
  return (
    <span
      className={`explorer-cell-value explorer-cell-${cell.kind}`}
      title={title}
    >
      {cell.value}
      {cell.truncated ? (
        <span className="explorer-truncated" title="Value truncated by MiniBase">
          … [truncated]
        </span>
      ) : null}
    </span>
  )
}

function ColumnsTable({ description }) {
  if (description.columns.length === 0) {
    return <div className="empty-state">This object has no visible columns.</div>
  }
  return (
    <div className="explorer-table-scroll">
      <table className="explorer-table explorer-columns-table">
        <thead>
          <tr>
            <th>Column</th>
            <th>Type</th>
            <th>Nullable</th>
            <th>Primary Key</th>
          </tr>
        </thead>
        <tbody>
          {description.columns.map((column) => (
            <tr key={column.name}>
              <td><code>{column.name}</code></td>
              <td>{column.dataType}</td>
              <td>{column.nullable ? 'Yes' : 'No'}</td>
              <td>{column.primaryKey ? <strong className="explorer-pk">PK</strong> : '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function DataTable({ page }) {
  if (page.rows.length === 0) {
    return <div className="empty-state">No rows returned.</div>
  }
  return (
    <div className="explorer-table-scroll" data-testid="explorer-data-grid">
      <table className="explorer-table explorer-data-table">
        <thead>
          <tr>
            {page.columns.map((column) => <th key={column}>{column}</th>)}
          </tr>
        </thead>
        <tbody>
          {page.rows.map((row, rowIndex) => (
            <tr key={`${page.offset}-${rowIndex}`}>
              {row.map((cell, cellIndex) => (
                <td key={`${page.columns[cellIndex]}-${cellIndex}`}>
                  <ExplorerCell cell={cell} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default function DatabaseExplorerPage({ api, databaseID, navigate }) {
  const [database, setDatabase] = useState(null)
  const [catalog, setCatalog] = useState({ schemas: [] })
  const [selected, setSelected] = useState(null)
  const [description, setDescription] = useState(null)
  const [page, setPage] = useState(null)
  const [activeTab, setActiveTab] = useState('data')
  const [pageSize, setPageSize] = useState(50)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)
  const [resourceLoading, setResourceLoading] = useState(false)
  const [error, setError] = useState('')
  const [resourceError, setResourceError] = useState('')

  useEffect(() => {
    let cancelled = false
    async function loadExplorer() {
      setLoading(true)
      try {
        const loadedDatabase = await api.getDatabase(databaseID)
        if (cancelled) return
        setDatabase(loadedDatabase)
        if (loadedDatabase.status !== 'ready') {
          setCatalog({ schemas: [] })
          setSelected(null)
          setError('')
          return
        }
        const loadedCatalog = await api.getExplorerObjects(databaseID)
        if (cancelled) return
        setCatalog(loadedCatalog)
        setSelected(firstObject(loadedCatalog))
        setError('')
      } catch (requestError) {
        if (!cancelled) {
          setError(safeErrorMessage(
            requestError,
            'Unable to load Database Explorer.',
          ))
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void loadExplorer()
    return () => { cancelled = true }
  }, [api, databaseID])

  const selectedKey = objectKey(selected)
  useEffect(() => {
    if (!selected) {
      setDescription(null)
      setPage(null)
      return undefined
    }
    let cancelled = false
    async function loadResource() {
      setResourceLoading(true)
      setDescription(null)
      setPage(null)
      setResourceError('')
      try {
        const [loadedDescription, loadedPage] = await Promise.all([
          api.getExplorerColumns(databaseID, selected.schema, selected.name),
          api.getExplorerRows(
            databaseID,
            selected.schema,
            selected.name,
            pageSize,
            offset,
          ),
        ])
        if (cancelled) return
        setDescription(loadedDescription)
        setPage(loadedPage)
        setResourceError('')
      } catch (requestError) {
        if (!cancelled) {
          setDescription(null)
          setPage(null)
          setResourceError(safeErrorMessage(
            requestError,
            'Unable to load the selected database object.',
          ))
        }
      } finally {
        if (!cancelled) setResourceLoading(false)
      }
    }
    void loadResource()
    return () => { cancelled = true }
  }, [api, databaseID, offset, pageSize, selectedKey])

  const objectCount = useMemo(
    () => catalog.schemas.reduce((count, schema) => count + schema.objects.length, 0),
    [catalog],
  )

  if (loading && !database) {
    return <div className="empty-state">Loading Database Explorer…</div>
  }

  if (error || !database) {
    return (
      <section className="detail-error">
        <p className="eyebrow">DATABASE EXPLORER</p>
        <h1>Explorer unavailable</h1>
        <p>{error || 'Unable to load Database Explorer.'}</p>
        <AppLink className="button secondary" href={routes.databases} navigate={navigate}>
          Back to databases
        </AppLink>
      </section>
    )
  }

  return (
    <div className="explorer-page">
      <section className="page-hero compact explorer-hero">
        <div>
          <AppLink className="explorer-back-link" href={routes.databases} navigate={navigate}>
            ← Databases
          </AppLink>
          <p className="eyebrow">ADMIN / DATABASE EXPLORER</p>
          <h1>{database.displayName}</h1>
          <p>Inspect schemas, columns, and stored rows without changing database contents.</p>
        </div>
        <div className="explorer-hero-status">
          <span className="read-only-badge">READ ONLY</span>
          <StatusBadge status={database.status} />
        </div>
      </section>

      {database.status !== 'ready' ? (
        <div className="notice error">
          Database Explorer is available only when this database is ready.
        </div>
      ) : (
        <section className="explorer-layout" aria-label="Database Explorer">
          <aside className="explorer-object-sidebar" aria-label="Database objects">
            <div className="explorer-sidebar-heading">
              <div>
                <p className="eyebrow">DATABASE OBJECTS</p>
                <h2>Schemas</h2>
              </div>
              <span className="record-count">{objectCount}</span>
            </div>
            <div className="explorer-schema-list">
              {catalog.schemas.map((schema) => (
                <section className="explorer-schema" key={schema.name}>
                  <h3>{schema.name}</h3>
                  {schema.objects.length === 0 ? (
                    <p className="explorer-schema-empty">No supported objects</p>
                  ) : schema.objects.map((object) => {
                    const current = selected?.schema === schema.name &&
                      selected?.name === object.name
                    return (
                      <button
                        className={`explorer-object${current ? ' active' : ''}`}
                        type="button"
                        aria-pressed={current}
                        key={`${schema.name}.${object.name}`}
                        onClick={() => {
                          setOffset(0)
                          setSelected({ schema: schema.name, ...object })
                        }}
                      >
                        <span>{object.name}</span>
                        <small>{objectLabel(object.type)}</small>
                      </button>
                    )
                  })}
                </section>
              ))}
            </div>
          </aside>

          <div className="explorer-resource-panel">
            {!selected ? (
              <div className="empty-state">No tables or views are available.</div>
            ) : (
              <>
                <header className="explorer-resource-header">
                  <div>
                    <p className="eyebrow">SELECTED OBJECT</p>
                    <h2>{selected.schema}.{selected.name}</h2>
                  </div>
                  <span className="explorer-object-type">{objectLabel(selected.type)}</span>
                </header>

                <div className="explorer-tabs" role="tablist" aria-label="Explorer views">
                  <button
                    type="button"
                    role="tab"
                    aria-selected={activeTab === 'columns'}
                    className={activeTab === 'columns' ? 'active' : ''}
                    onClick={() => setActiveTab('columns')}
                  >
                    Columns
                  </button>
                  <button
                    type="button"
                    role="tab"
                    aria-selected={activeTab === 'data'}
                    className={activeTab === 'data' ? 'active' : ''}
                    onClick={() => setActiveTab('data')}
                  >
                    Data
                  </button>
                </div>

                {resourceError ? <div className="notice error">{resourceError}</div> : null}
                {resourceLoading && !description ? (
                  <div className="empty-state">Loading {selected.name}…</div>
                ) : activeTab === 'columns' && description ? (
                  <ColumnsTable description={description} />
                ) : activeTab === 'data' && page ? (
                  <>
                    <DataTable page={page} />
                    <div className="explorer-pagination">
                      <label>
                        Rows per page
                        <select
                          value={pageSize}
                          onChange={(event) => {
                            setOffset(0)
                            setPageSize(Number(event.target.value))
                          }}
                        >
                          {pageSizes.map((size) => <option value={size} key={size}>{size}</option>)}
                        </select>
                      </label>
                      <span>
                        {page.rows.length === 0
                          ? 'Showing 0'
                          : `Showing ${page.offset + 1}–${page.offset + page.rows.length}`}
                      </span>
                      <div className="explorer-page-actions">
                        <button
                          className="button secondary compact-button"
                          type="button"
                          disabled={page.offset === 0 || resourceLoading}
                          onClick={() => setOffset(Math.max(0, page.offset - page.limit))}
                        >
                          ← Previous
                        </button>
                        <button
                          className="button secondary compact-button"
                          type="button"
                          disabled={!page.hasMore || resourceLoading}
                          onClick={() => setOffset(page.offset + page.limit)}
                        >
                          Next →
                        </button>
                      </div>
                    </div>
                  </>
                ) : null}
              </>
            )}
          </div>
        </section>
      )}
    </div>
  )
}
