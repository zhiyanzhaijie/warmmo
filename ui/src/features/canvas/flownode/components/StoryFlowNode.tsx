import type { NodeProps } from '@xyflow/react'
import { memo, type ReactNode } from 'react'

import { CandidateFlowNodeActions } from '@/features/canvas/flownode/components/CandidateFlowNodeActions'
import { FlowNodeControls } from '@/features/canvas/flownode/components/FlowNodeControls'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'
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
  const Icon = definition.icon

  return (
    <article className={`group relative w-64 overflow-hidden rounded-sm border bg-canvas-elevated transition-colors ${selected ? 'border-link' : 'border-hairline'}`}>
      <div className={`absolute inset-x-0 top-0 h-0.5 ${definition.accentClassName}`} />
      <header className="flex h-10 items-center justify-between border-b border-hairline px-space-sm">
        <span className="flex min-w-0 items-center gap-space-xs text-label-sm">
          <Icon className="shrink-0 text-mute" size={15} />
          <span className="truncate">{data.title}</span>
        </span>
        <span className="font-mono text-body-sm text-mute">{definition.label}</span>
      </header>
      <div className="max-h-44 overflow-hidden px-space-sm py-space-sm text-body-sm leading-5 text-body">
        {children}
      </div>
      {data.sourceType === 'candidate' ? (
        <CandidateFlowNodeActions
          workId={data.workId}
          candidateId={data.sourceId}
          title={data.title}
        />
      ) : null}
      <footer className="flex h-8 items-center justify-between border-t border-hairline bg-hairline-soft/50 px-space-sm font-mono text-[0.6875rem] text-mute">
        <span>{data.sourceType === 'candidate' ? 'CANDIDATE' : `REV ${data.revision}`}</span>
        <span>{data.layerId}</span>
      </footer>
      <FlowNodeControls />
    </article>
  )
}
