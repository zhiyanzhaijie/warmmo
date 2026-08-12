import { Plus } from 'lucide-react'

interface NewWorkCardProps {
  onCreate: () => void
}

export function NewWorkCard({ onCreate }: NewWorkCardProps) {
  return (
    <button
      className="group relative flex min-h-56 cursor-pointer flex-col items-center justify-center rounded-md text-body transition-colors hover:bg-hairline-soft/60 hover:text-ink"
      type="button"
      onClick={onCreate}
    >
      <svg aria-hidden="true" className="pointer-events-none absolute inset-0 size-full text-hairline transition-colors group-hover:text-mute">
        <rect x="0.5" y="0.5" width="calc(100% - 1px)" height="calc(100% - 1px)" fill="none" rx="11.5" stroke="currentColor" strokeDasharray="4 8" strokeWidth="1" />
      </svg>
      <span className="grid size-12 place-items-center rounded-full shadow-whisper transition-transform group-hover:scale-105">
        <Plus size={36} aria-hidden="true" />
      </span>
      <span className="mt-space-sm text-button-md">新建空白工作</span>
    </button>
  )
}
