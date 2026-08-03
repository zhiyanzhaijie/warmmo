import { ArrowUpRight, LoaderCircle } from 'lucide-react'
import { Link } from 'react-router-dom'

import type { WorkSummary } from '../../types/work'
import { CanvasThumbnail } from './CanvasThumbnail'

interface WorkCardProps {
  work: WorkSummary
}

export function WorkCard({ work }: WorkCardProps) {
  return (
    <Link
      className="group overflow-hidden rounded-md border border-hairline bg-canvas-elevated text-ink no-underline shadow-whisper transition-[border-color,box-shadow] hover:border-faint hover:shadow-floating"
      to={`/works/${work.id}`}
    >
      <CanvasThumbnail nodes={work.previewNodes} edges={work.previewEdges} />
      <div className="p-space-md">
        <div className="flex items-start justify-between gap-space-sm">
          <div className="min-w-0">
            <h3 className="truncate text-label-sm">{work.title}</h3>
            <p className="mt-space-xxs text-body-sm text-mute">{work.nodeCount} 个节点 · {work.updatedLabel}</p>
          </div>
          {work.status === 'initializing' ? (
            <LoaderCircle className="shrink-0 animate-spin text-link" size={16} aria-label="初始化中" />
          ) : (
            <ArrowUpRight className="shrink-0 text-faint transition-colors group-hover:text-ink" size={16} aria-hidden="true" />
          )}
        </div>
        <p className="mt-space-sm truncate border-t border-hairline pt-space-sm font-mono text-body-sm text-mute">{work.modelName}</p>
      </div>
    </Link>
  )
}
