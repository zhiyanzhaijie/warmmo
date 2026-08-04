import type { NodeProps } from '@xyflow/react'
import { memo } from 'react'

import { FlowNodeControls } from '@/features/canvas/flownode/components/FlowNodeControls'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'

export const CompactFlowNodeRenderer = memo(function CompactFlowNodeRenderer({ data, selected }: NodeProps<StoryFlowNode>) {
  const definition = nodeDefinitions[data.kind]
  const Icon = definition.icon

  return (
    <article className={`group relative flex h-12 w-52 items-center gap-space-sm overflow-hidden rounded-sm border bg-canvas-elevated px-space-sm shadow-whisper ${selected ? 'border-link' : 'border-hairline'}`}>
      <span className={`absolute inset-y-0 left-0 w-0.5 ${definition.accentClassName}`} />
      <Icon className="shrink-0 text-mute" size={14} />
      <span className="truncate text-label-sm">{data.title}</span>
      <FlowNodeControls />
    </article>
  )
})

export const MarkerFlowNodeRenderer = memo(function MarkerFlowNodeRenderer({ data, selected }: NodeProps<StoryFlowNode>) {
  const definition = nodeDefinitions[data.kind]
  return (
    <div
      aria-label={data.title}
      className={`group relative size-5 rounded-full border-2 border-canvas shadow-whisper ${definition.accentClassName} ${selected ? 'ring-2 ring-link ring-offset-1 ring-offset-canvas' : ''}`}
      title={data.title}
    >
      <FlowNodeControls />
    </div>
  )
})
