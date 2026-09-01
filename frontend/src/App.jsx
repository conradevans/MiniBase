import { useCallback, useEffect, useState } from 'react'

import { adminApi as defaultAdminApi } from './api/admin'
import { guestApi as defaultGuestApi } from './api/guest'
import AdminShell from './components/AdminShell'
import BackupsPage from './components/BackupsPage'
import DatabaseDetailPage from './components/DatabaseDetailPage'
import DatabasesPage from './components/DatabasesPage'
import GuestPage from './components/GuestPage'
import LandingPage from './components/LandingPage'
import NotFoundPage from './components/NotFoundPage'
import OverviewPage from './components/OverviewPage'
import { resolveRoute } from './routing'

import './App.css'

export default function App({
  initialPathname,
  adminApi = defaultAdminApi,
  guestApi = defaultGuestApi,
}) {
  const [pathname, setPathname] = useState(
    () => initialPathname ?? window.location.pathname,
  )

  useEffect(() => {
    function handlePopState() {
      setPathname(window.location.pathname)
    }
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [])

  const navigate = useCallback((nextPath) => {
    if (window.location.pathname !== nextPath) {
      window.history.pushState({}, '', nextPath)
    }
    setPathname(nextPath)
  }, [])

  const route = resolveRoute(pathname)
  switch (route.screen) {
    case 'landing':
      return <LandingPage navigate={navigate} />
    case 'guest':
      return <GuestPage api={guestApi} navigate={navigate} />
    case 'overview':
      return (
        <AdminShell active="overview" navigate={navigate}>
          <OverviewPage api={adminApi} navigate={navigate} />
        </AdminShell>
      )
    case 'databases':
      return (
        <AdminShell active="databases" navigate={navigate}>
          <DatabasesPage api={adminApi} navigate={navigate} />
        </AdminShell>
      )
    case 'backups':
      return (
        <AdminShell active="backups" navigate={navigate}>
          <BackupsPage api={adminApi} navigate={navigate} />
        </AdminShell>
      )
    case 'database-detail':
      return (
        <AdminShell active="databases" navigate={navigate}>
          <DatabaseDetailPage
            api={adminApi}
            databaseID={route.databaseID}
            navigate={navigate}
          />
        </AdminShell>
      )
    default:
      return <NotFoundPage navigate={navigate} />
  }
}
