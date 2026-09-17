export const routes = {
  home: '/',
  guest: '/guest',
  admin: '/admin',
  databases: '/admin/databases',
  visibility: '/admin/visibility',
  backups: '/admin/backups',
  activity: '/admin/activity',
}

export function databaseDetailPath(id) {
  return `${routes.databases}/${encodeURIComponent(id)}`
}

export function databaseExplorerPath(id) {
  return `${databaseDetailPath(id)}/explorer`
}

export function resolveRoute(pathname) {
  const normalized = normalizePath(pathname)
  switch (normalized) {
    case routes.home:
      return { screen: 'landing' }
    case routes.guest:
      return { screen: 'guest' }
    case routes.admin:
      return { screen: 'overview' }
    case routes.databases:
      return { screen: 'databases' }
    case routes.visibility:
      return { screen: 'visibility' }
    case routes.backups:
      return { screen: 'backups' }
    case routes.activity:
      return { screen: 'activity' }
    default:
      break
  }

  const prefix = `${routes.databases}/`
  if (!normalized.startsWith(prefix)) {
    return { screen: 'not-found' }
  }
  const parts = normalized.slice(prefix.length).split('/')
  if (parts.length < 1 || parts.length > 2 || !parts[0]) {
    return { screen: 'not-found' }
  }
  const nestedRoute = parts.length === 2 ? parts[1] : ''
  if (nestedRoute && nestedRoute !== 'explorer') {
    return { screen: 'not-found' }
  }
  try {
    const databaseID = decodeURIComponent(parts[0])
    if (!databaseID || databaseID === '.' || databaseID === '..' || databaseID.includes('/')) {
      return { screen: 'not-found' }
    }
    return {
      screen: nestedRoute === 'explorer'
        ? 'database-explorer'
        : 'database-detail',
      databaseID,
    }
  } catch {
    return { screen: 'not-found' }
  }
}

function normalizePath(pathname) {
  if (typeof pathname !== 'string' || pathname === '') {
    return '/'
  }
  if (pathname === '/') {
    return pathname
  }
  return pathname.endsWith('/') ? pathname.slice(0, -1) : pathname
}
