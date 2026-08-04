import { NodeToolbar, Position, useStore } from '@xyflow/react'
import { ArrowUp, LoaderCircle, Paperclip, Plus, Sparkles } from 'lucide-react'
import { memo, type ReactNode, useCallback, useState } from 'react'

import { useCreateAgentRun } from '@/apis/canvas-apis'
import { ModelSelector } from '@/components/models/ModelSelector'
import { agentEventLabels, getAgentEventSummary } from '@/features/canvas/agent-events'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'
import { useAgentRunStream } from '@/features/canvas/use-agent-run-stream'
import type { EnabledModel } from '@/types/provider'

export const CanvasAgentWorkspace = memo(function CanvasAgentWorkspace({ workId }: { workId: string }) {
  const createRun = useCreateAgentRun(workId)
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const toolbarSourceNodeId = useFlowNodeStore((state) => state.toolbarSourceNodeId)
  const selectedNodeKind = useFlowNodeStore((state) => {
    if (state.toolbarSourceNodeId === null) return null
    return state.nodes.find((node) =>
      node.data.sourceType === 'node' && node.data.sourceId === state.toolbarSourceNodeId)?.data.kind ?? null
  })
  const isNodeDragging = useStore((state) => state.nodes.some((node) => node.dragging))
  const [model, setModel] = useState<EnabledModel | null>(null)
  const [prompt, setPrompt] = useState('让主角在宴会上第一次发现记忆能力的代价。')
  const { activeRunId, draft, events, streamError, streamRun } = useAgentRunStream(workId)
  const canRun = model !== null && prompt.trim() !== '' && selectedNodeIds.length === 1 && activeRunId === null
  const runAgent = useCallback(() => {
    const targetNodeId = selectedNodeIds[0]
    if (model === null || targetNodeId === undefined || !canRun || createRun.isPending) return
    createRun.mutate({ prompt, targetNodeId, contextNodeIds: selectedNodeIds, model }, {
      onSuccess: (run) => streamRun(run.id),
    })
  }, [canRun, createRun, model, prompt, selectedNodeIds, streamRun])
  const nodeDefinition = selectedNodeKind === null ? null : nodeDefinitions[selectedNodeKind]
  const NodeIcon = nodeDefinition?.icon

  return (
    <>
      {selectedNodeIds.length === 1 && toolbarSourceNodeId === selectedNodeIds[0] && !isNodeDragging ? (
        <NodeToolbar
          nodeId={selectedNodeIds}
          position={Position.Bottom}
          offset={12}
          isVisible
          className="z-20"
        >
          <section className="nodrag nopan nowheel w-[min(calc(100vw_-_2rem),40rem)] overflow-hidden rounded-sm border border-hairline bg-canvas-elevated shadow-floating">
            <header className="flex h-10 items-center gap-space-xs px-space-md pt-space-xxs">
              {NodeIcon !== undefined ? <NodeIcon className="text-mute" size={14} /> : null}
              <span className="text-label-sm text-body">{nodeDefinition?.label ?? '节点'}节点</span>
              <span className="ml-auto font-mono text-mono-eyebrow text-faint">NODE</span>
            </header>

            <textarea
              className="block min-h-28 max-h-48 w-full resize-none bg-transparent px-space-md pb-space-sm text-body-md leading-6 text-ink outline-none placeholder:text-faint"
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              onKeyDown={(event) => {
                if (event.key !== 'Enter' || (!event.metaKey && !event.ctrlKey)) return
                event.preventDefault()
                runAgent()
              }}
              placeholder="告诉这个节点接下来要做什么"
            />

            <footer className="flex min-h-11 flex-wrap items-center justify-between gap-space-xs px-space-sm pb-space-sm">
              <div className="flex items-center gap-space-xxs">
                <ToolbarIconButton label="添加上下文" disabled><Plus size={15} /></ToolbarIconButton>
                <ToolbarIconButton label="附加素材" disabled><Paperclip size={15} /></ToolbarIconButton>
              </div>
              <div className="flex min-w-0 items-center gap-space-xxs">
                <ModelSelector
                  capability="text"
                  value={model}
                  onValueChange={setModel}
                  autoSelectFirst
                  compact
                  className="h-8 min-w-0 max-w-44 border-transparent bg-hairline-soft px-space-xs text-body-sm"
                  ariaLabel="选择当前节点使用的文本模型"
                />
                <button
                  aria-label={activeRunId === null ? '运行节点指令' : '节点指令运行中'}
                  className="grid size-8 shrink-0 place-items-center rounded-sm bg-primary text-on-primary transition-opacity hover:opacity-85 disabled:opacity-35"
                  type="button"
                  title="运行节点指令 · Cmd/Ctrl+Enter"
                  disabled={!canRun || createRun.isPending}
                  onClick={runAgent}
                >
                  {createRun.isPending || activeRunId !== null
                    ? <LoaderCircle className="animate-spin" size={15} />
                    : <ArrowUp size={15} strokeWidth={2.25} />}
                </button>
              </div>
            </footer>
          </section>
        </NodeToolbar>
      ) : null}

      {events.length > 0 || draft !== '' || streamError !== '' ? (
        <aside className="absolute top-space-md right-space-md z-20 flex max-h-[calc(100dvh-10rem)] w-72 flex-col rounded-sm border border-hairline bg-canvas-elevated shadow-floating">
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

function ToolbarIconButton({
  children,
  label,
  disabled = false,
}: {
  children: ReactNode
  label: string
  disabled?: boolean
}) {
  return (
    <button
      aria-label={label}
      className="grid size-8 place-items-center rounded-sm border-0 bg-hairline-soft text-mute transition-colors hover:text-ink disabled:cursor-not-allowed disabled:opacity-35"
      type="button"
      title={label}
      disabled={disabled}
    >
      {children}
    </button>
  )
}
