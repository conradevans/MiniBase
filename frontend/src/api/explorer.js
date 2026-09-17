import {
  requireBoolean,
  requireNumber,
  requireRecord,
  requireString,
} from './validation'

const objectTypes = new Set(['table', 'view', 'materialized_view'])
const cellKinds = new Set(['value', 'json', 'null', 'binary'])

function requireInteger(value) {
  const result = requireNumber(value)
  if (!Number.isInteger(result)) {
    throw new Error('unexpected response')
  }
  return result
}

function toExplorerObject(value) {
  const object = requireRecord(value)
  const type = requireString(object.type)
  if (!objectTypes.has(type)) {
    throw new Error('unexpected response')
  }
  return {
    name: requireString(object.name),
    type,
  }
}

export function toExplorerCatalog(value) {
  const catalog = requireRecord(value)
  if (!Array.isArray(catalog.schemas)) {
    throw new Error('unexpected response')
  }
  return {
    schemas: catalog.schemas.map((value) => {
      const schema = requireRecord(value)
      if (!Array.isArray(schema.objects)) {
        throw new Error('unexpected response')
      }
      return {
        name: requireString(schema.name),
        objects: schema.objects.map(toExplorerObject),
      }
    }),
  }
}

function toExplorerColumn(value) {
  const column = requireRecord(value)
  return {
    name: requireString(column.name),
    dataType: requireString(column.dataType),
    nullable: requireBoolean(column.nullable),
    primaryKey: requireBoolean(column.primaryKey),
    ordinalPosition: requireInteger(column.ordinalPosition),
  }
}

export function toExplorerDescription(value) {
  const description = requireRecord(value)
  if (!Array.isArray(description.columns)) {
    throw new Error('unexpected response')
  }
  return {
    schema: requireString(description.schema),
    object: toExplorerObject(description.object),
    columns: description.columns.map(toExplorerColumn),
  }
}

function toExplorerCell(value) {
  const cell = requireRecord(value)
  const kind = requireString(cell.kind)
  if (!cellKinds.has(kind)) {
    throw new Error('unexpected response')
  }
  return {
    kind,
    value: kind === 'null' ? '' : requireString(cell.value),
    truncated: requireBoolean(cell.truncated),
  }
}

export function toExplorerRowsPage(value) {
  const page = requireRecord(value)
  if (!Array.isArray(page.columns) || !Array.isArray(page.rows)) {
    throw new Error('unexpected response')
  }
  const columns = page.columns.map(requireString)
  const rows = page.rows.map((value) => {
    if (!Array.isArray(value) || value.length !== columns.length) {
      throw new Error('unexpected response')
    }
    return value.map(toExplorerCell)
  })
  return {
    schema: requireString(page.schema),
    object: toExplorerObject(page.object),
    columns,
    rows,
    limit: requireInteger(page.limit),
    offset: requireInteger(page.offset),
    hasMore: requireBoolean(page.hasMore),
  }
}

function objectQuery(schemaName, objectName) {
  return new URLSearchParams({ schema: schemaName, object: objectName })
}

export function createExplorerMethods(requester) {
  return {
    async getExplorerObjects(databaseID) {
      return toExplorerCatalog(
        await requester(
          `/api/v1/databases/${encodeURIComponent(databaseID)}/explorer/objects`,
        ),
      )
    },

    async getExplorerColumns(databaseID, schemaName, objectName) {
      const query = objectQuery(schemaName, objectName)
      return toExplorerDescription(
        await requester(
          `/api/v1/databases/${encodeURIComponent(databaseID)}/explorer/columns?${query}`,
        ),
      )
    },

    async getExplorerRows(
      databaseID,
      schemaName,
      objectName,
      limit = 50,
      offset = 0,
    ) {
      const query = objectQuery(schemaName, objectName)
      query.set('limit', String(limit))
      query.set('offset', String(offset))
      return toExplorerRowsPage(
        await requester(
          `/api/v1/databases/${encodeURIComponent(databaseID)}/explorer/rows?${query}`,
        ),
      )
    },
  }
}
