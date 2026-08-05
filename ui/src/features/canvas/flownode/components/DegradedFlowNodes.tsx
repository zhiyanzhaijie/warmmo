import type { NodeProps } from '@xyflow/react'
import { memo } from 'react'

import { FlowNodeLabel } from '@/features/canvas/flownode/components/FlowNodeLabel'
import { FlowNodeControls } from '@/features/canvas/flownode/components/FlowNodeControls'
import {
  NodeAgentCompactIndicator,
  NodeAgentMarkerIndicator,
} from '@/features/canvas/flownode/components/NodeAgentExecution'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'

export const CompactFlowNodeRenderer = memo(function CompactFlowNodeRenderer({ data, selected }: NodeProps<StoryFlowNode>) {
  const definition = nodeDefinitions[data.kind]

  return (
    <article className="group relative w-52 overflow-visible">
      <FlowNodeLabel data={data} selected={selected} />
      <div className="relative flex h-12 items-center gap-space-sm overflow-hidden rounded-sm border border-hairline bg-canvas-elevated px-space-sm">
        <p className="line-clamp-2 min-w-0 text-body-sm leading-4 text-body">{data.content}</p>
        {data.sourceType === 'node' ? <NodeAgentCompactIndicator nodeId={data.sourceId} /> : null}
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 rounded-[inherit]"
          style={{ boxShadow: selected ? `inset 0 0 0 1px ${definition.accentColor}` : undefined }}
        />
      </div>
      <FlowNodeControls />
    </article>
  )
})

export const MarkerFlowNodeRenderer = memo(function MarkerFlowNodeRenderer({ data, selected }: NodeProps<StoryFlowNode>) {
  const definition = nodeDefinitions[data.kind]
  return (
    <div
      aria-label={data.title}
      className="group relative size-5 overflow-visible"
      title={data.title}
    >
      <span className={`relative block size-full overflow-hidden rounded-full border-2 border-canvas ${definition.accentClassName}`}>
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 rounded-[inherit]"
          style={{ boxShadow: selected ? `inset 0 0 0 1px ${definition.accentColor}` : undefined }}
        />
      </span>
      {data.sourceType === 'node' ? <NodeAgentMarkerIndicator nodeId={data.sourceId} /> : null}
      <FlowNodeControls />
    </div>
  )
})
