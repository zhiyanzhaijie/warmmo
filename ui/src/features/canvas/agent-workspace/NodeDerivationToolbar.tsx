import { NodeToolbar, Position, useStore } from '@xyflow/react'
import { Bomb, LoaderCircle } from 'lucide-react'
import { memo, useCallback, useMemo } from 'react'

import {
  type NodeDerivationTarget,
  useCreateNodeDerivationRun,
} from '@/apis/canvas-apis'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useAgentRunStream } from '@/features/canvas/agent-workspace/hook'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { CanvasNodeKind } from '@/types/canvas'
import type { EnabledModel } from '@/types/provider'

interface NodeDerivationToolbarProps {
  workId: string
  model: EnabledModel | null
}

interface DerivationDefinition {
  target: NodeDerivationTarget
  prompt: string
  label: string
}

const derivations: Partial<Record<CanvasNodeKind, DerivationDefinition>> = {
  'chapter-outline': {
    target: 'section-outline-batch',
    prompt: '根据当前章节概览和提供的画布上下文，拆分为数量合理、前后连续且可独立编写的子章节规划。',
    label: '拆分子章节规划',
  },
  'section-outline': {
    target: 'chapter-section',
    prompt: '根据当前子章节规划和提供的画布上下文，完成这一小节的正文。',
    label: '生成章节小节',
  },
}

export const NodeDerivationToolbar = memo(function NodeDerivationToolbar({
  workId,
  model,
}: NodeDerivationToolbarProps) {
  const createRun = useCreateNodeDerivationRun(workId)
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const toolbarSourceNodeId = useFlowNodeStore((state) => state.toolbarSourceNodeId)
  const nodes = useFlowNodeStore((state) => state.nodes)
  const edges = useFlowNodeStore((state) => state.edges)
  const beginNodeAgentRun = useFlowNodeStore((state) => state.actions.beginNodeAgentRun)
  const failNodeAgentRun = useFlowNodeStore((state) => state.actions.failNodeAgentRun)
  const isNodeDragging = useStore((state) => state.nodes.some((node) => node.dragging))
  const { streamRun } = useAgentRunStream(workId)
  const targetNodeId = selectedNodeIds[0] ?? null
  const targetAgentRun = useFlowNodeStore((state) => targetNodeId === null
    ? undefined
    : state.nodeAgentRuns[targetNodeId])
  const nodeById = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes])
  const targetNode = useMemo(() => targetNodeId === null
    ? undefined
    : nodeById.get(targetNodeId),
  [nodeById, targetNodeId])
  const definition = targetNode === undefined ? undefined : derivations[targetNode.data.kind]
  const derivedKind = targetNode?.data.kind === 'chapter-outline' ? 'section-outline' : 'chapter-section'
  const hasDerivedChildren = targetNodeId !== null && edges.some((edge) => {
    if (edge.source !== targetNodeId || edge.data?.kind !== 'generated_from') return false
    const child = nodeById.get(edge.target)
    return child?.data.sourceType === 'node' && child.data.kind === derivedKind
  })
  const contextNodeIds = useMemo(() => {
    if (targetNodeId === null || targetNode === undefined) return []
    if (targetNode.data.kind === 'chapter-outline') return [targetNodeId]

    const chapterOutlineNodeIds = edges.flatMap((edge) => {
      if (edge.target !== targetNodeId || edge.data?.kind !== 'generated_from') return []
      const sourceNode = nodeById.get(edge.source)
      return sourceNode?.data.sourceType === 'node' && sourceNode.data.kind === 'chapter-outline'
        ? [edge.source]
        : []
    })
    const chapterOutlineNodeIdSet = new Set(chapterOutlineNodeIds)
    const chapterContextNodeIds = edges.flatMap((edge) =>
      chapterOutlineNodeIdSet.has(edge.target) && edge.data?.kind === 'generated_from'
        ? [edge.source]
        : [])
    return [...new Set([targetNodeId, ...chapterOutlineNodeIds, ...chapterContextNodeIds])]
  }, [edges, nodeById, targetNode, targetNodeId])
  const hasBlockingRun = targetAgentRun?.status === 'submitting' ||
    targetAgentRun?.status === 'running' || targetAgentRun?.status === 'waiting_input'
  const isCreatingTargetRun = createRun.isPending && createRun.variables?.targetNodeId === targetNodeId
  const isPending = isCreatingTargetRun || targetAgentRun?.status === 'submitting' || targetAgentRun?.status === 'running'
  const disabled = model === null || definition === undefined || hasDerivedChildren || hasBlockingRun || isPending

  const derive = useCallback(() => {
    if (targetNodeId === null || definition === undefined || model === null || disabled) return
    beginNodeAgentRun(targetNodeId, 'derive')
    createRun.mutate({
      prompt: definition.prompt,
      target: definition.target,
      targetNodeId,
      contextNodeIds,
      model,
    }, {
      onSuccess: (run) => streamRun(run.id, targetNodeId),
      onError: (error) => failNodeAgentRun(
        targetNodeId,
        error instanceof Error ? error.message : '无法创建节点派生任务',
      ),
    })
  }, [beginNodeAgentRun, contextNodeIds, createRun, definition, disabled, failNodeAgentRun, model, streamRun, targetNodeId])

  if (targetNodeId === null || targetNode === undefined || definition === undefined ||
    toolbarSourceNodeId !== targetNodeId || isNodeDragging) return null

  const tooltip = model === null
    ? '请先在节点输入框中选择文本模型'
    : hasDerivedChildren
      ? '当前节点已经生成过下一层节点'
      : hasBlockingRun
        ? '当前节点已有 Agent Run 正在执行'
        : definition.label

  return (
    <NodeToolbar
      nodeId={targetNodeId}
      position={Position.Right}
      offset={14}
      isVisible
      className="z-20"
    >
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="nodrag nopan inline-flex">
              <Button
                type="button"
                size="icon-sm"
                variant="outline"
                aria-label={definition.label}
                disabled={disabled}
                className="rounded-sm border-hairline bg-canvas-elevated text-ink shadow-floating hover:bg-canvas-subtle"
                onClick={derive}
              >
                {isPending ? (
                  <LoaderCircle aria-hidden="true" className="animate-spin" size={15} />
                ) : (
                  <Bomb aria-hidden="true" size={15} />
                )}
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent side="right">{tooltip}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </NodeToolbar>
  )
})
