import { NodeToolbar, Position, useStore } from '@xyflow/react'
import { Archive, Bomb, LoaderCircle } from 'lucide-react'
import { memo, useCallback, useMemo } from 'react'
import { toast } from 'sonner'

import {
  type NodeDerivationTarget,
  useCreateNodeDerivationRun,
} from '@/apis/canvas-apis'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useAgentRunStream } from '@/features/canvas/agent-workspace/hook'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { useArchiveLocks } from '@/features/canvas/story-spine/use-archive-locks'
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
  blocksWhenDerived: boolean
  requiresChildKind?: CanvasNodeKind
}

const derivations: Partial<Record<CanvasNodeKind, DerivationDefinition[]>> = {
  'chapter-outline': [{
    target: 'section-outline-batch',
    prompt: '根据当前章节概览和提供的画布上下文，拆分为数量合理、前后连续且可独立编写的子章节规划。',
    label: '拆分子章节规划',
    blocksWhenDerived: true,
  }, {
    target: 'chapter-archive',
    prompt: '归档当前整章，综合所有已完成的小节正文，分析本章事件对已有角色、地点、物品和其他实体造成的状态变化，并提出需要作者确认的版本候选。',
    label: '归档整章并同步实体',
    blocksWhenDerived: false,
    requiresChildKind: 'chapter-section',
  }],
  'section-outline': [{
    target: 'chapter-section',
    prompt: '根据当前子章节规划和提供的画布上下文，完成这一小节的正文。',
    label: '生成章节小节',
    blocksWhenDerived: true,
  }],
}

export const NodeDerivationToolbar = memo(function NodeDerivationToolbar({
  workId,
  model,
}: NodeDerivationToolbarProps) {
  const createRun = useCreateNodeDerivationRun(workId)
  const archiveLocks = useArchiveLocks(workId)
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const toolbarSourceNodeId = useFlowNodeStore((state) => state.toolbarSourceNodeId)
  const nodes = useFlowNodeStore((state) => state.nodes)
  const edges = useFlowNodeStore((state) => state.edges)
  const beginNodeAgentRun = useFlowNodeStore((state) => state.actions.beginNodeAgentRun)
  const dismissNodeAgentRun = useFlowNodeStore((state) => state.actions.dismissNodeAgentRun)
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
  const isTargetArchiveLocked = targetNodeId !== null && archiveLocks.lockedNodeIds.has(targetNodeId)
  const definitions = targetNode === undefined ? [] : (derivations[targetNode.data.kind] ?? [])
  const hasChildOfKind = useCallback((kind: CanvasNodeKind) => targetNodeId !== null && edges.some((edge) => {
    if (edge.source !== targetNodeId || edge.data?.kind !== 'generated_from') return false
    const child = nodeById.get(edge.target)
    return child?.data.sourceType === 'node' && child.data.kind === kind
  }), [edges, nodeById, targetNodeId])
  const hasArchiveSections = useMemo(() => {
    if (targetNodeId === null) return false
    if (hasChildOfKind('chapter-section')) return true
    const sectionOutlineIds = new Set(edges.flatMap((edge) => {
      if (edge.source !== targetNodeId || edge.data?.kind !== 'generated_from') return []
      const child = nodeById.get(edge.target)
      return child?.data.sourceType === 'node' && child.data.kind === 'section-outline' ? [child.id] : []
    }))
    return edges.some((edge) => sectionOutlineIds.has(edge.source) && edge.data?.kind === 'generated_from' &&
      nodeById.get(edge.target)?.data.sourceType === 'node' && nodeById.get(edge.target)?.data.kind === 'chapter-section')
  }, [edges, hasChildOfKind, nodeById, targetNodeId])
  const hasRequiredChildren = useCallback((definition: DerivationDefinition) => {
    if (definition.requiresChildKind === undefined) return true
    if (definition.requiresChildKind === 'chapter-section' && targetNode?.data.kind === 'chapter-outline') return hasArchiveSections
    return hasChildOfKind(definition.requiresChildKind)
  }, [hasArchiveSections, hasChildOfKind, targetNode])
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
  const derive = useCallback((definition: DerivationDefinition) => {
    const disabled = !archiveLocks.isResolved || isTargetArchiveLocked || model === null || !hasRequiredChildren(definition) ||
      (definition.blocksWhenDerived && hasChildOfKind(
        targetNode?.data.kind === 'chapter-outline' ? 'section-outline' : 'chapter-section',
      )) || hasBlockingRun || isPending
    if (targetNodeId === null || model === null || disabled) return
    beginNodeAgentRun(targetNodeId, 'derive')
    createRun.mutate({
      prompt: definition.prompt,
      target: definition.target,
      targetNodeId,
      contextNodeIds,
      model,
    }, {
      onSuccess: (run) => streamRun(run.id, targetNodeId),
      onError: (error) => {
        toast.error(error instanceof Error ? error.message : '无法创建节点派生任务')
        dismissNodeAgentRun(targetNodeId)
      },
    })
  }, [archiveLocks.isResolved, beginNodeAgentRun, contextNodeIds, createRun, dismissNodeAgentRun, hasBlockingRun, hasChildOfKind, hasRequiredChildren, isPending, isTargetArchiveLocked, model, streamRun, targetNodeId, targetNode])

  if (!archiveLocks.isResolved || isTargetArchiveLocked || targetNodeId === null || targetNode === undefined || definitions.length === 0 ||
    toolbarSourceNodeId !== targetNodeId || isNodeDragging) return null

  return (
    <NodeToolbar
      nodeId={targetNodeId}
      position={Position.Right}
      offset={14}
      isVisible
      className="z-20"
    >
      <TooltipProvider>
        <div className="flex flex-col gap-space-xs">
          {definitions.map((definition) => {
            const requiredChildrenExist = hasRequiredChildren(definition)
            const hasBlockingDerivedChildren = definition.blocksWhenDerived && hasChildOfKind(
              targetNode.data.kind === 'chapter-outline' ? 'section-outline' : 'chapter-section',
            )
            const disabled = model === null || !requiredChildrenExist || hasBlockingDerivedChildren || hasBlockingRun || isPending
            const tooltip = model === null
              ? '请先在节点输入框中选择文本模型'
              : !requiredChildrenExist
                ? '请先完成该章节的子章节正文'
                : hasBlockingDerivedChildren
                  ? '当前节点已经生成过下一层节点'
                  : hasBlockingRun
                    ? '当前节点已有 Agent Run 正在执行'
                    : definition.label
            return (
              <Tooltip key={definition.target}>
                <TooltipTrigger asChild>
                  <span className="nodrag nopan inline-flex">
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="outline"
                      aria-label={definition.label}
                      disabled={disabled}
                      className="rounded-sm border-hairline bg-canvas-elevated text-ink shadow-floating hover:bg-canvas-subtle"
                      onClick={() => derive(definition)}
                    >
                      {isPending
                        ? <LoaderCircle aria-hidden="true" className="animate-spin" size={15} />
                        : definition.target === 'chapter-archive'
                          ? <Archive aria-hidden="true" size={15} />
                          : <Bomb aria-hidden="true" size={15} />}
                    </Button>
                  </span>
                </TooltipTrigger>
                <TooltipContent side="right">{tooltip}</TooltipContent>
              </Tooltip>
            )
          })}
        </div>
      </TooltipProvider>
    </NodeToolbar>
  )
})
