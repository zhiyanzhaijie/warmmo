import { NodeToolbar, Position, useStore } from '@xyflow/react'
import { memo, useCallback, useMemo, useState } from 'react'

import { useCreateAgentRun, useRespondToAgentRun } from '@/apis/canvas-apis'
import { CanvasAgentPromptInput, type PendingAgentInput } from '@/features/canvas/agent-workspace/AgentPromptInput'
import { useAgentRunStream } from '@/features/canvas/agent-workspace/hook'
import { type NodeAgentRunState, useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { EnabledModel } from '@/types/provider'

interface CanvasAgentWorkspaceProps {
  workId: string
  model: EnabledModel | null
  onModelChange: (model: EnabledModel | null) => void
}

export const CanvasAgentWorkspace = memo(function CanvasAgentWorkspace({
  workId,
  model,
  onModelChange,
}: CanvasAgentWorkspaceProps) {
  const createRun = useCreateAgentRun(workId)
  const respondToRun = useRespondToAgentRun()
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const toolbarSourceNodeId = useFlowNodeStore((state) => state.toolbarSourceNodeId)
  const flowNodes = useFlowNodeStore((state) => state.nodes)
  const flowEdges = useFlowNodeStore((state) => state.edges)
  const targetNodeId = selectedNodeIds[0] ?? null
  const contextNodeIds = useMemo(() => targetNodeId === null
    ? []
    : [...new Set(flowEdges.flatMap((edge) =>
        edge.target === targetNodeId && edge.data?.kind === 'generated_from' ? [edge.source] : []))],
  [flowEdges, targetNodeId])
  const sourceNodeById = useMemo(() => new Map(flowNodes.flatMap((node) =>
    node.data.sourceType === 'node' ? [[node.data.sourceId, node] as const] : [])), [flowNodes])
  const targetNode = targetNodeId === null ? undefined : sourceNodeById.get(targetNodeId)
  const targetAgentRun = useFlowNodeStore((state) => targetNodeId === null ? undefined : state.nodeAgentRuns[targetNodeId])
  const hasBlockingAgentRun = targetAgentRun?.status === 'submitting' ||
    targetAgentRun?.status === 'running' || targetAgentRun?.status === 'waiting_input'
  const pendingInput = useMemo(() => getPendingAgentInput(targetAgentRun), [targetAgentRun])
  const contextNodes = useMemo(() => contextNodeIds.flatMap((nodeId) => {
    const node = sourceNodeById.get(nodeId)
    return node === undefined ? [] : [node]
  }), [contextNodeIds, sourceNodeById])
  const isNodeDragging = useStore((state) => state.nodes.some((node) => node.dragging))
  const [prompt, setPrompt] = useState('让主角在宴会上第一次发现记忆能力的代价。')
  const beginNodeAgentRun = useFlowNodeStore((state) => state.actions.beginNodeAgentRun)
  const failNodeAgentRun = useFlowNodeStore((state) => state.actions.failNodeAgentRun)
  const { streamRun } = useAgentRunStream(workId)
  const isCreatingTargetRun = createRun.isPending && createRun.variables?.targetNodeId === targetNodeId
  const canRun = model !== null && prompt.trim() !== '' && targetNodeId !== null && !hasBlockingAgentRun
  const runAgent = useCallback((nextPrompt: string) => {
    if (model === null || targetNodeId === null || !canRun || isCreatingTargetRun) return
    const runContextNodeIds = [...new Set([...contextNodeIds, targetNodeId])]
    beginNodeAgentRun(targetNodeId)
    createRun.mutate({ prompt: nextPrompt, targetNodeId, contextNodeIds: runContextNodeIds, model }, {
      onSuccess: (run) => streamRun(run.id, targetNodeId),
      onError: (error) => failNodeAgentRun(
        targetNodeId,
        error instanceof Error ? error.message : '无法创建 Agent Run',
      ),
    })
  }, [beginNodeAgentRun, canRun, contextNodeIds, createRun, failNodeAgentRun, isCreatingTargetRun, model, streamRun, targetNodeId])
  const respond = useCallback((answer: string) => {
    if (pendingInput === null || targetNodeId === null || respondToRun.isPending) return
    respondToRun.mutate({
      runId: pendingInput.runId,
      approvalEventId: pendingInput.approvalEventId,
      answer,
    }, {
      onSuccess: () => streamRun(pendingInput.runId, targetNodeId, pendingInput.lastSequence),
    })
  }, [pendingInput, respondToRun, streamRun, targetNodeId])
  return targetNodeId !== null && targetNode !== undefined && targetNode.data.archiveStateResolved &&
    !targetNode.data.archiveLocked && toolbarSourceNodeId === targetNodeId && !isNodeDragging ? (
    <NodeToolbar
      nodeId={targetNodeId}
      position={Position.Bottom}
      offset={12}
      isVisible
      className="z-20"
    >
      <section
        data-node-kind={targetNode.data.kind}
        className="nodrag nopan nowheel w-[min(calc(100vw_-_2rem),40rem)] overflow-hidden rounded-sm bg-canvas-elevated shadow-floating"
      >
        <CanvasAgentPromptInput
          canSubmit={canRun && !isCreatingTargetRun}
          contextNodes={contextNodes}
          hasError={targetAgentRun?.status === 'failed' || respondToRun.isError}
          isStreaming={targetAgentRun?.status === 'running'}
          isSubmitting={isCreatingTargetRun || targetAgentRun?.status === 'submitting'}
          isResponding={respondToRun.isPending}
          model={model}
          nodeKind={targetNode.data.kind}
          pendingInput={pendingInput}
          prompt={prompt}
          onModelChange={onModelChange}
          onPromptChange={setPrompt}
          onRespond={respond}
          onSubmit={runAgent}
        />
      </section>
    </NodeToolbar>
  ) : null
})

function getPendingAgentInput(run: NodeAgentRunState | undefined): PendingAgentInput | null {
  if (run?.status !== 'waiting_input' || run.runId === null) return null
  const answeredApprovalIds = new Set(run.events.flatMap((event) => {
    if (event.type !== 'user.response.received') return []
    const approvalEventId = event.data?.approvalEventId
    return typeof approvalEventId === 'string' ? [approvalEventId] : []
  }))
  const event = run.events.findLast((candidate) => candidate.type === 'approval.required' && !answeredApprovalIds.has(candidate.id))
  if (event === undefined) return null
  const question = event.data?.question
  const options = event.data?.options
  return {
    runId: run.runId,
    approvalEventId: event.id,
    question: typeof question === 'string' ? question : 'Agent 需要你补充信息',
    options: Array.isArray(options) ? options.filter((option): option is string => typeof option === 'string') : [],
    lastSequence: run.events.reduce((latest, current) => Math.max(latest, current.sequence), 0),
  }
}
