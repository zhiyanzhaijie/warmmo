import { Outlet } from 'react-router-dom'

import { AppHeader } from '../components/navigation/AppHeader'

export function AppLayout() {
  return (
    <div className="min-h-dvh bg-canvas text-ink">
      <AppHeader />
      <Outlet />
    </div>
  )
}
