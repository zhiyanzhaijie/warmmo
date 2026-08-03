import { Plus } from 'lucide-react'

interface NewWorkCardProps {
  onCreate: () => void
}

export function NewWorkCard({ onCreate }: NewWorkCardProps) {
  return (
    <button
      className="group flex min-h-72 cursor-pointer flex-col items-center justify-center rounded-md border border-dashed border-hairline bg-canvas-elevated text-body transition-colors hover:border-faint hover:bg-hairline-soft hover:text-ink"
      type="button"
      onClick={onCreate}
    >
      <span className="grid size-10 place-items-center rounded-full border border-hairline bg-canvas shadow-whisper transition-transform group-hover:scale-105">
        <Plus size={17} aria-hidden="true" />
      </span>
      <span className="mt-space-sm text-button-md">新建空白工作</span>
    </button>
  )
}
