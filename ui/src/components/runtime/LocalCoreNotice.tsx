import { ArrowRight, ServerOff } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'

import { useRuntimeInfo } from '@/apis/runtime-apis'

export function LocalCoreNotice() {
  const location = useLocation()
  const { state } = useRuntimeInfo()
  if (state.status !== 'error' || location.pathname.endsWith('/runtime')) return null

  return (
    <aside className="border-b border-hairline bg-hairline-soft" aria-label="本地 Core 状态">
      <div className="mx-auto flex min-h-10 max-w-app items-center gap-space-xs px-space-lg text-body-sm text-body">
        <ServerOff className="shrink-0 text-error" size={14} aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate">本地 Core 尚未连接，部分功能暂不可用。</span>
        <Link className="flex shrink-0 items-center gap-space-xxs text-label-sm text-link no-underline hover:underline" to="/runtime">
          检查服务
          <ArrowRight size={13} aria-hidden="true" />
        </Link>
      </div>
    </aside>
  )
}
