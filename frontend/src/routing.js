export const routes = {
  home: '/',
  guest: '/guest',
  admin: '/admin',
  databases: '/admin/databases',
}

export function databaseDetailPath(id) {
  return `${routes.databases}/${encodeURIComponent(id)}`
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
    default:
      break
  }

  const prefix = `${routes.databases}/`
  if (!normalized.startsWith(prefix)) {
    return { screen: 'not-found' }
  }
  const encodedID = normalized.slice(prefix.length)
  if (!encodedID || encodedID.includes('/')) {
    return { screen: 'not-found' }
  }
  try {
    const databaseID = decodeURIComponent(encodedID)
    if (!databaseID || databaseID === '.' || databaseID === '..' || databaseID.includes('/')) {
      return { screen: 'not-found' }
    }
    return { screen: 'database-detail', databaseID }
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
