import { ChevronDown, ChevronRight, LayoutGrid, LoaderCircle, LockKeyhole } from 'lucide-react'
import { memo } from 'react'

import { nodeDefinitions } from '@/features/canvas/nodes/definitions'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'

export const FlowNodeLabel = memo(function FlowNodeLabel({
  data,
  selected,
}: {
  data: StoryFlowNode['data']
  selected: boolean
}) {
  const definition = nodeDefinitions[data.kind]
  const Icon = definition.icon
  const accentColor = data.archiveLocked ? 'var(--color-archive)' : definition.accentColor

  return (
    <div className="pointer-events-none absolute right-0 bottom-full left-0 mb-space-xxs flex h-6 min-w-0 items-center gap-space-xs select-none">
      <Icon aria-hidden="true" className="shrink-0" size={14} style={{ color: accentColor }} />
      <span
        className="min-w-0 truncate text-body-sm transition-colors"
        style={{ color: selected || data.archiveLocked ? accentColor : 'var(--color-mute)' }}
      >
        {data.title}
      </span>
      <span className="shrink-0 font-mono text-[0.625rem]" style={{ color: accentColor }}>
        {definition.label}
      </span>
      {data.archiveLocked ? <LockKeyhole aria-hidden="true" className="shrink-0" size={12} style={{ color: accentColor }} /> : null}
      {data.onLayoutArchive !== undefined ? (
        <button
          type="button"
          aria-label="整理归档章节布局"
          aria-busy={data.archiveLayoutPending}
          className="nodrag nopan pointer-events-auto ml-auto grid size-5 shrink-0 place-items-center rounded-sm transition-colors hover:bg-canvas-elevated focus-visible:outline-1 disabled:cursor-wait disabled:opacity-60"
          disabled={data.archiveLayoutDisabled}
          style={{ color: accentColor }}
          title="整理归档章节布局"
          onClick={(event) => {
            event.stopPropagation()
            data.onLayoutArchive?.(data.sourceId)
          }}
        >
          {data.archiveLayoutPending
            ? <LoaderCircle aria-hidden="true" className="animate-spin" size={13} />
            : <LayoutGrid aria-hidden="true" size={13} />}
        </button>
      ) : null}
      {data.archiveExpanded !== undefined && data.onToggleArchive !== undefined ? (
        <button
          type="button"
          aria-expanded={data.archiveExpanded}
          aria-label={data.archiveExpanded ? '收起归档子章节' : '展开归档子章节'}
          className="nodrag nopan pointer-events-auto grid size-5 shrink-0 place-items-center rounded-sm transition-colors hover:bg-canvas-elevated focus-visible:outline-1"
          style={{ color: accentColor }}
          title={data.archiveExpanded ? '收起归档子章节' : '展开归档子章节'}
          onClick={(event) => {
            event.stopPropagation()
            data.onToggleArchive?.(data.sourceId)
          }}
        >
          {data.archiveExpanded
            ? <ChevronDown aria-hidden="true" size={14} />
            : <ChevronRight aria-hidden="true" size={14} />}
        </button>
      ) : null}
    </div>
  )
})
