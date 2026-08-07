import { NodeToolbar, Position, useStore } from '@xyflow/react'
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import {
  useCreateAgentRun,
  useCreateCanvasEdge,
  useDeleteCanvasEdges,
  useRespondToAgentRun,
} from '@/apis/canvas-apis'
import {
  CanvasAgentPromptInput,
  type CanvasAgentPromptSubmission,
  type PendingAgentInput,
} from '@/features/canvas/agent-workspace/AgentPromptInput'
import {
  createCanvasPromptValueFromText,
  type CanvasPromptValue,
} from '@/features/canvas/agent-workspace/CanvasPromptEditor'
import { useAgentRunStream } from '@/features/canvas/agent-workspace/hook'
import {
  getContextNodePickerTargetNodeId,
  type NodeAgentRunState,
  useFlowNodeStore,
} from '@/features/canvas/flownode/store'
import type { CanvasEdge, CanvasNode } from '@/types/canvas'
import type { EnabledModel } from '@/types/provider'

const emptyContextNodeIdSet: ReadonlySet<string> = new Set()
const defaultPrompt = createCanvasPromptValueFromText('让主角在宴会上第一次发现记忆能力的代价。')

interface CanvasAgentWorkspaceProps {
  canvasEdges: CanvasEdge[]
  canvasNodes: CanvasNode[]
  workId: string
  model: EnabledModel | null
  onModelChange: (model: EnabledModel | null) => void
}

export const CanvasAgentWorkspace = memo(function CanvasAgentWorkspace({
  canvasEdges,
  canvasNodes,
  workId,
  model,
  onModelChange,
}: CanvasAgentWorkspaceProps) {
  const createRun = useCreateAgentRun(workId)
  const respondToRun = useRespondToAgentRun()
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const toolbarSourceNodeId = useFlowNodeStore((state) => state.toolbarSourceNodeId)
  const flowNodes = useFlowNodeStore((state) => state.nodes)
  const canvasInteractionMode = useFlowNodeStore((state) => state.canvasInteractionMode)
  const contextNodePickerTargetNodeId = getContextNodePickerTargetNodeId(canvasInteractionMode)
  const targetNodeId = selectedNodeIds[0] ?? null
  const sourceNodeById = useMemo(() => new Map(flowNodes.flatMap((node) =>
    node.data.sourceType === 'node' ? [[node.data.sourceId, node] as const] : [])), [flowNodes])
  const targetNode = targetNodeId === null ? undefined : sourceNodeById.get(targetNodeId)
  const targetAgentRun = useFlowNodeStore((state) => targetNodeId === null ? undefined : state.nodeAgentRuns[targetNodeId])
  const hasBlockingAgentRun = targetAgentRun?.status === 'submitting' ||
    targetAgentRun?.status === 'running' || targetAgentRun?.status === 'waiting_input'
  const pendingInput = useMemo(() => getPendingAgentInput(targetAgentRun), [targetAgentRun])
  const canvasNodeById = useMemo(() => new Map(canvasNodes.map((node) => [node.id, node])), [canvasNodes])
  const attachmentNodeIds = useMemo(() => {
    if (targetNodeId === null) return []
    const nodeIds = new Set<string>()
    for (const edge of canvasEdges) {
      if (edge.kind === 'generated_from' && edge.targetNodeId === targetNodeId) nodeIds.add(edge.sourceNodeId)
    }
    return [...nodeIds]
  }, [canvasEdges, targetNodeId])
  const attachmentNodeIdSet = useMemo(() => new Set(attachmentNodeIds), [attachmentNodeIds])
  const attachmentNodes = useMemo(() => attachmentNodeIds.flatMap((nodeId) => {
    const node = canvasNodeById.get(nodeId)
    return node === undefined ? [] : [node]
  }), [attachmentNodeIds, canvasNodeById])
  const availableContextNodes = useMemo(() =>
    canvasNodes.filter((node) => node.id !== targetNodeId),
  [canvasNodes, targetNodeId])
  const isNodeDragging = useStore((state) => state.nodes.some((node) => node.dragging))
  const [promptDrafts, setPromptDrafts] = useState<Record<string, CanvasPromptValue>>({})
  const prompt = targetNodeId === null ? defaultPrompt : promptDrafts[targetNodeId] ?? defaultPrompt
  const [pendingAttachmentEdgeKeys, setPendingAttachmentEdgeKeys] = useState<ReadonlySet<string>>(() => new Set())
  const pendingAttachmentEdgeKeysRef = useRef<ReadonlySet<string>>(new Set())
  const beginNodeAgentRun = useFlowNodeStore((state) => state.actions.beginNodeAgentRun)
  const cancelContextNodePicker = useFlowNodeStore((state) => state.actions.cancelContextNodePicker)
  const dismissNodeAgentRun = useFlowNodeStore((state) => state.actions.dismissNodeAgentRun)
  const startContextNodePicker = useFlowNodeStore((state) => state.actions.startContextNodePicker)
  const { streamRun } = useAgentRunStream(workId)
  const { mutate: deleteEdges } = useDeleteCanvasEdges(workId)
  const { mutate: createContextEdge } = useCreateCanvasEdge(workId)
  const isCreatingTargetRun = createRun.isPending && createRun.variables?.targetNodeId === targetNodeId
  const isContextPicking = canvasInteractionMode.kind === 'context-node-picker' &&
    canvasInteractionMode.targetNodeId === targetNodeId
  const pendingAttachmentNodeIds = useMemo(() => {
    if (targetNodeId === null) return emptyContextNodeIdSet
    const prefix = `${targetNodeId}\u0000`
    const nodeIds = new Set<string>()
    for (const key of pendingAttachmentEdgeKeys) {
      if (key.startsWith(prefix)) nodeIds.add(key.slice(prefix.length))
    }
    return nodeIds
  }, [pendingAttachmentEdgeKeys, targetNodeId])
  const hasPendingAttachment = pendingAttachmentNodeIds.size > 0
  const canRun = model !== null && prompt.requestText.trim() !== '' && targetNodeId !== null && !hasBlockingAgentRun && !hasPendingAttachment

  useEffect(() => {
    if (targetNodeId === null || attachmentNodeIds.length === 0) return
    setPendingAttachmentEdgeKeys((current) => {
      let next: Set<string> | null = null
      for (const nodeId of attachmentNodeIds) {
        const key = getAttachmentEdgeKey(targetNodeId, nodeId)
        if (!current.has(key)) continue
        if (next === null) next = new Set(current)
        next.delete(key)
      }
      if (next === null) return current
      pendingAttachmentEdgeKeysRef.current = next
      return next
    })
  }, [attachmentNodeIds, targetNodeId])

  useEffect(() => {
    if (contextNodePickerTargetNodeId !== null && (
      contextNodePickerTargetNodeId !== targetNodeId || hasBlockingAgentRun
    )) cancelContextNodePicker()
  }, [cancelContextNodePicker, contextNodePickerTargetNodeId, hasBlockingAgentRun, targetNodeId])

  const toggleContextNodePicker = useCallback(() => {
    if (targetNodeId === null) return
    if (contextNodePickerTargetNodeId === targetNodeId) {
      cancelContextNodePicker()
      return
    }
    startContextNodePicker(targetNodeId)
  }, [cancelContextNodePicker, contextNodePickerTargetNodeId, startContextNodePicker, targetNodeId])

  const removeContextNode = useCallback((nodeId: string) => {
    if (targetNodeId === null) return
    const edgeIds = canvasEdges.flatMap((edge) =>
      edge.sourceNodeId === nodeId && edge.targetNodeId === targetNodeId && edge.kind === 'generated_from'
        ? [edge.id]
        : [])
    if (edgeIds.length > 0) deleteEdges(edgeIds)
  }, [canvasEdges, deleteEdges, targetNodeId])

  const addPriorityContextNode = useCallback((nodeId: string) => {
    if (targetNodeId === null || nodeId === targetNodeId || attachmentNodeIdSet.has(nodeId)) return
    const key = getAttachmentEdgeKey(targetNodeId, nodeId)
    if (pendingAttachmentEdgeKeysRef.current.has(key)) return

    const nextPendingAttachmentEdgeKeys = new Set(pendingAttachmentEdgeKeysRef.current)
    nextPendingAttachmentEdgeKeys.add(key)
    pendingAttachmentEdgeKeysRef.current = nextPendingAttachmentEdgeKeys
    setPendingAttachmentEdgeKeys(nextPendingAttachmentEdgeKeys)
    createContextEdge({ sourceNodeId: nodeId, targetNodeId }, {
      onError: () => {
        if (!pendingAttachmentEdgeKeysRef.current.has(key)) return
        const next = new Set(pendingAttachmentEdgeKeysRef.current)
        next.delete(key)
        pendingAttachmentEdgeKeysRef.current = next
        setPendingAttachmentEdgeKeys(next)
      },
    })
  }, [attachmentNodeIdSet, createContextEdge, targetNodeId])

  const updatePromptDraft = useCallback((value: CanvasPromptValue) => {
    if (targetNodeId === null) return
    setPromptDrafts((current) => current[targetNodeId] === value
      ? current
      : { ...current, [targetNodeId]: value })
  }, [targetNodeId])

  const runAgent = useCallback((input: CanvasAgentPromptSubmission) => {
    if (model === null || targetNodeId === null || !canRun || isCreatingTargetRun) return
    const runContextNodeIds = [...new Set([
      targetNodeId,
      ...input.contextNodeIds.filter((nodeId) => attachmentNodeIdSet.has(nodeId)),
    ])]
    beginNodeAgentRun(targetNodeId)
    createRun.mutate({ prompt: input.prompt, targetNodeId, contextNodeIds: runContextNodeIds, model }, {
      onSuccess: (run) => streamRun(run.id, targetNodeId),
      onError: (error) => {
        toast.error(error instanceof Error ? error.message : '无法创建 Agent Run')
        dismissNodeAgentRun(targetNodeId)
      },
    })
  }, [attachmentNodeIdSet, beginNodeAgentRun, canRun, createRun, dismissNodeAgentRun, isCreatingTargetRun, model, streamRun, targetNodeId])
  const respond = useCallback((answer: string) => {
    if (pendingInput === null || targetNodeId === null || respondToRun.isPending) return
    respondToRun.mutate({
      runId: pendingInput.runId,
      approvalEventId: pendingInput.approvalEventId,
      answer,
    }, {
      onSuccess: () => streamRun(pendingInput.runId, targetNodeId, pendingInput.lastSequence),
      onError: (error) => toast.error(error instanceof Error ? error.message : '提交回答失败'),
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
        className="nodrag nopan nowheel w-[min(calc(100vw_-_2rem),40rem)] overflow-visible rounded-[calc(var(--radius-md)+var(--spacing-space-sm))] bg-canvas-elevated shadow-floating"
      >
        <CanvasAgentPromptInput
          key={targetNodeId}
          attachmentNodeIds={attachmentNodeIdSet}
          attachmentNodes={attachmentNodes}
          availableContextNodes={availableContextNodes}
          canSubmit={canRun && !isCreatingTargetRun}
          hasError={respondToRun.isError}
          isContextPicking={isContextPicking}
          isStreaming={targetAgentRun?.status === 'running'}
          isSubmitting={isCreatingTargetRun || targetAgentRun?.status === 'submitting'}
          isResponding={respondToRun.isPending}
          model={model}
          nodeKind={targetNode.data.kind}
          pendingAttachmentNodeIds={pendingAttachmentNodeIds}
          pendingInput={pendingInput}
          prompt={prompt}
          onContextNodeRemove={removeContextNode}
          onContextPickerToggle={toggleContextNodePicker}
          onModelChange={onModelChange}
          onPromptChange={updatePromptDraft}
          onPriorityContextNodeAdd={addPriorityContextNode}
          onRespond={respond}
          onSubmit={runAgent}
        />
      </section>
    </NodeToolbar>
  ) : null
})

function getAttachmentEdgeKey(targetNodeId: string, nodeId: string) {
  return `${targetNodeId}\u0000${nodeId}`
}

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
