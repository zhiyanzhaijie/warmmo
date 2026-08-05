import type { NodeProps } from '@xyflow/react'
import { memo, type ReactNode } from 'react'

import { CandidateFlowNodeActions } from '@/features/canvas/flownode/components/CandidateFlowNodeActions'
import { FlowNodeLabel } from '@/features/canvas/flownode/components/FlowNodeLabel'
import { FlowNodeControls } from '@/features/canvas/flownode/components/FlowNodeControls'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'
import { NodeAgentExecution } from '@/features/canvas/flownode/components/NodeAgentExecution'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'

export const StoryFlowNodeRenderer = memo(function StoryFlowNodeRenderer({ data, selected }: NodeProps<StoryFlowNode>) {
  return (
    <FlowNodeShell data={data} selected={selected}>
      <p className="line-clamp-7 whitespace-pre-wrap">{data.content}</p>
    </FlowNodeShell>
  )
})

function FlowNodeShell({
  data,
  selected,
  children,
}: {
  data: StoryFlowNode['data']
  selected: boolean
  children: ReactNode
}) {
  const definition = nodeDefinitions[data.kind]

  return (
    <article className="group relative w-64 overflow-visible">
      <FlowNodeLabel data={data} selected={selected} />
      <div className="relative overflow-hidden rounded-sm border border-hairline bg-canvas-elevated">
        <div className="min-h-20 max-h-44 overflow-hidden px-space-sm py-space-sm text-body-sm leading-5 text-body">
          {children}
        </div>
        {data.sourceType === 'node' ? <NodeAgentExecution nodeId={data.sourceId} /> : null}
        {data.sourceType === 'candidate' ? (
          <CandidateFlowNodeActions
            workId={data.workId}
            candidateId={data.sourceId}
            title={data.title}
          />
        ) : null}
        <span
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 rounded-[inherit]"
          style={{ boxShadow: selected ? `inset 0 0 0 1px ${definition.accentColor}` : undefined }}
        />
      </div>
      <FlowNodeControls />
    </article>
  )
}
