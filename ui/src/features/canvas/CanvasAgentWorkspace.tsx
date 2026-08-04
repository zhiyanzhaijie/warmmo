import { NodeToolbar, Position, useStore } from '@xyflow/react'
import { LoaderCircle, Play, Sparkles } from 'lucide-react'
import { memo, type ReactNode, useState } from 'react'

import { useCreateAgentRun } from '@/apis/canvas-apis'
import { ModelSelector } from '@/components/models/ModelSelector'
import { agentEventLabels, getAgentEventSummary } from '@/features/canvas/agent-events'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { useAgentRunStream } from '@/features/canvas/use-agent-run-stream'
import type { EnabledModel } from '@/types/provider'

export const CanvasAgentWorkspace = memo(function CanvasAgentWorkspace({ workId }: { workId: string }) {
  const createRun = useCreateAgentRun(workId)
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const isNodeDragging = useStore((state) => state.nodes.some((node) => node.dragging))
  const [model, setModel] = useState<EnabledModel | null>(null)
  const [prompt, setPrompt] = useState('让主角在宴会上第一次发现记忆能力的代价。')
  const { activeRunId, draft, events, streamError, streamRun } = useAgentRunStream(workId)
  const canRun = model !== null && prompt.trim() !== '' && selectedNodeIds.length === 1 && activeRunId === null
  const composer = (
    <AgentPromptComposer>
      <textarea
        className="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-space-xs py-2 outline-none placeholder:text-faint"
        value={prompt}
        onChange={(event) => setPrompt(event.target.value)}
        placeholder="输入创作指令"
      />
      <button
        className="flex h-10 shrink-0 items-center gap-space-xs rounded-sm bg-primary px-space-md text-button-md text-on-primary disabled:opacity-40"
        type="button"
        disabled={!canRun || createRun.isPending}
        onClick={() => {
          if (model === null) return
          createRun.mutate({ prompt, targetNodeId: selectedNodeIds[0], contextNodeIds: selectedNodeIds, model }, {
            onSuccess: (run) => streamRun(run.id),
          })
        }}
      >
        {createRun.isPending || activeRunId !== null
          ? <LoaderCircle className="animate-spin" size={16} />
          : <Play size={16} fill="currentColor" />}
        {activeRunId === null ? '生成' : '运行中'}
      </button>
    </AgentPromptComposer>
  )

  return (
    <>
      <div className="absolute top-0 right-16 z-30 flex h-16 items-center">
        <ModelSelector
          capability="text"
          value={model}
          onValueChange={setModel}
          autoSelectFirst
          className="max-w-56"
        />
      </div>

      {selectedNodeIds.length === 1 && !isNodeDragging ? (
        <NodeToolbar
          nodeId={selectedNodeIds}
          position={Position.Bottom}
          offset={12}
          isVisible
          className="z-20"
        >
          {composer}
        </NodeToolbar>
      ) : null}

      {events.length > 0 || draft !== '' || streamError !== '' ? (
        <aside className="absolute top-20 right-space-md z-20 flex max-h-[calc(100dvh-15rem)] w-72 flex-col rounded-sm border border-hairline bg-canvas-elevated shadow-floating">
          <header className="flex h-11 shrink-0 items-center justify-between border-b border-hairline px-space-md">
            <span className="flex items-center gap-space-xs text-label-sm"><Sparkles size={15} />Agent</span>
            <span className="font-mono text-body-sm text-mute">{events.length}</span>
          </header>
          <ol className="overflow-y-auto p-space-md">
            {events.map((event) => (
              <li key={event.id} className="relative border-l border-hairline pb-space-md pl-space-md last:pb-0">
                <span className="absolute -left-1 top-1 size-2 rounded-full bg-link" />
                <div className="text-label-sm">{agentEventLabels[event.type] ?? event.type}</div>
                <div className="mt-1 break-words font-mono text-body-sm text-mute">{getAgentEventSummary(event)}</div>
              </li>
            ))}
          </ol>
          {draft !== '' ? (
            <div className="max-h-40 overflow-y-auto border-t border-hairline p-space-md text-body-sm leading-5 text-body">
              {draft}
            </div>
          ) : null}
          {streamError !== '' ? (
            <div className="border-t border-hairline p-space-sm text-body-sm text-error">{streamError}</div>
          ) : null}
        </aside>
      ) : null}
    </>
  )
})

function AgentPromptComposer({ children }: { children: ReactNode }) {
  return (
    <section className="nodrag nopan nowheel flex w-[min(calc(100vw_-_2rem),48rem)] items-end gap-space-sm rounded-sm border border-hairline bg-canvas-elevated p-space-sm shadow-floating">
      {children}
    </section>
  )
}
