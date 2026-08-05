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

  return (
    <div className="pointer-events-none absolute bottom-full left-0 mb-space-xxs flex h-6 min-w-0 max-w-full items-center gap-space-xs select-none">
      <Icon aria-hidden="true" className="shrink-0" size={14} style={{ color: definition.accentColor }} />
      <span
        className="min-w-0 truncate text-body-sm transition-colors"
        style={{ color: selected ? definition.accentColor : 'var(--color-mute)' }}
      >
        {data.title}
      </span>
      <span className="shrink-0 font-mono text-[0.625rem]" style={{ color: definition.accentColor }}>
        {definition.label}
      </span>
    </div>
  )
})
