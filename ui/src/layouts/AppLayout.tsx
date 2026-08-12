import { Outlet } from 'react-router-dom'

import { AppHeader } from '../components/navigation/AppHeader'
import { LocalCoreNotice } from '../components/runtime/LocalCoreNotice'

export function AppLayout() {
  return (
    <div className="min-h-dvh bg-canvas text-ink">
      <AppHeader />
      <LocalCoreNotice />
      <Outlet />
    </div>
  )
}
