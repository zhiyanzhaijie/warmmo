import { PanelsTopLeft, Settings, Waypoints } from 'lucide-react'
import { Link, NavLink } from 'react-router-dom'

import { useRuntimeInfo } from '../../hooks/useRuntimeInfo'

const navigationItems = [
  { key: 'home', label: '首页', href: '/', icon: Waypoints },
  { key: 'workspace', label: '工作空间', href: '/workspace', icon: PanelsTopLeft },
  { key: 'settings', label: '设置', href: '/settings', icon: Settings },
] satisfies Array<{ key: string; label: string; href: string; icon: typeof Waypoints }>

export function AppHeader() {
  const { state } = useRuntimeInfo()
  const isReady = state.status === 'success'
  const statusLabel = state.status === 'loading' ? 'Core 连接中' : isReady ? 'Core 就绪' : 'Core 离线'

  return (
    <header className="sticky top-0 z-20 border-b border-hairline bg-canvas/90 backdrop-blur-md">
      <div className="mx-auto grid h-16 max-w-app grid-cols-[1fr_auto_1fr] items-center px-space-lg">
        <Link className="flex w-fit items-center gap-space-sm text-label-sm text-ink no-underline" to="/" aria-label="Warmnote 首页">
          <span className="grid size-8 place-items-center rounded-sm bg-primary text-on-primary">
            <Waypoints size={17} aria-hidden="true" />
          </span>
          <span>Warmnote</span>
        </Link>

        <nav className="flex items-center gap-space-xxs" aria-label="主导航">
          {navigationItems.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.key}
                className={({ isActive }) => `flex h-10 items-center gap-space-xs rounded-sm px-space-sm text-button-md no-underline transition-colors ${isActive ? 'bg-primary text-on-primary' : 'text-body hover:bg-hairline-soft hover:text-ink'}`}
                to={item.href}
                end={item.key === 'home'}
              >
                <Icon size={15} aria-hidden="true" />
                <span>{item.label}</span>
              </NavLink>
            )
          })}
        </nav>

        <div className="flex items-center justify-end gap-space-xs text-body-sm text-mute" title={statusLabel}>
          <span className={`size-2 rounded-full ${isReady ? 'bg-link' : state.status === 'loading' ? 'bg-warning' : 'bg-error'}`} />
          <span>{statusLabel}</span>
        </div>
      </div>
    </header>
  )
}
