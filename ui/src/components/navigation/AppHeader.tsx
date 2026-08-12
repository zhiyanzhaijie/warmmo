import { House, PanelsTopLeft, Settings } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, NavLink } from 'react-router-dom'

import { useRuntimeInfo } from '@/apis/runtime-apis'

import { ThemeMenu } from '../theme/ThemeMenu'
import { Warmmo, WarmmoAnimated } from '../svgs/warmmo'

const navigationItems = [
  { key: 'home', label: '首页', href: '/', icon: House },
  { key: 'workspace', label: '工作空间', href: '/workspace', icon: PanelsTopLeft },
  { key: 'settings', label: '设置', href: '/settings', icon: Settings },
] satisfies Array<{ key: string; label: string; href: string; icon: typeof House }>

export function AppHeader() {
  const { state } = useRuntimeInfo()
  const [scrolled, setScrolled] = useState(() => typeof window !== 'undefined' && window.scrollY > 0)
  const isReady = state.status === 'success'
  const statusLabel = state.status === 'loading' ? 'Core 连接中' : isReady ? 'Core 就绪' : 'Core 离线'

  useEffect(() => {
    const handleScroll = () => {
      const nextScrolled = window.scrollY > 0
      setScrolled((current) => current === nextScrolled ? current : nextScrolled)
    }
    window.addEventListener('scroll', handleScroll, { passive: true })
    handleScroll()
    return () => window.removeEventListener('scroll', handleScroll)
  }, [])

  return (
    <header className="sticky top-0 z-20 bg-canvas">
      <div className="relative mx-auto grid h-16 max-w-app grid-cols-[1fr_auto_1fr] items-center px-space-lg">
        <Link className="flex w-fit items-center gap-space-xs text-label-sm text-ink no-underline transition-opacity hover:opacity-70" to="/" aria-label="Warmmo 首页">
          <Warmmo />
          <span className="hidden sm:inline">屋墨</span>
        </Link>

        <nav className="flex h-full items-center gap-space-xs" aria-label="主导航">
          {navigationItems.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.key}
                className={({ isActive }) => `relative flex h-10 items-center gap-space-xs px-space-xs text-button-md no-underline transition-colors after:absolute after:right-space-xs after:bottom-0 after:left-space-xs after:h-0.5 after:rounded-full after:transition-colors sm:px-space-sm sm:after:right-space-sm sm:after:left-space-sm ${isActive ? 'text-ink after:bg-ink' : 'text-mute after:bg-transparent hover:text-ink'}`}
                to={item.href}
                end={item.key === 'home'}
              >
                <Icon size={15} aria-hidden="true" />
                <span className="hidden sm:inline">{item.label}</span>
              </NavLink>
            )
          })}
        </nav>

        <div className="flex items-center justify-end gap-space-xs">
          <Link className="flex h-9 items-center gap-space-xs px-space-xs text-body-sm text-mute no-underline hover:text-ink" to="/runtime" title={statusLabel}>
            <span className={`size-1.5 rounded-full ${isReady ? 'bg-link' : state.status === 'loading' ? 'bg-warning' : 'bg-error'}`} />
            <span className="hidden lg:inline">{statusLabel}</span>
          </Link>
          <ThemeMenu />
        </div>
        <span
          className={`pointer-events-none absolute right-space-lg bottom-0 left-space-lg h-px bg-[linear-gradient(90deg,transparent,var(--color-hairline)_18%,var(--color-hairline)_82%,transparent)] transition-opacity duration-200 ${scrolled ? 'opacity-100' : 'opacity-0'}`}
          aria-hidden="true"
        />
      </div>
    </header>
  )
}
