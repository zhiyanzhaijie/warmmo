import { ArrowLeft } from 'lucide-react'
import { Outlet, useNavigate } from 'react-router-dom'

export function CanvasLayout() {
  const navigate = useNavigate()

  return (
    <div className="min-h-dvh bg-canvas text-ink">
      <button
        className="fixed top-space-md left-space-md z-50 grid size-10 cursor-pointer place-items-center rounded-sm border border-hairline bg-canvas-elevated text-ink shadow-floating transition-colors hover:bg-hairline-soft"
        type="button"
        onClick={() => navigate(-1)}
        title="返回上一页"
      >
        <ArrowLeft size={17} aria-hidden="true" />
        <span className="sr-only">返回上一页</span>
      </button>
      <Outlet />
    </div>
  )
}
