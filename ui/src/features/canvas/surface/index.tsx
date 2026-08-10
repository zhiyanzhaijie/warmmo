import {
  Background,
  BackgroundVariant,
  ReactFlow,
  SelectionMode,
  type NodeMouseHandler,
  type OnNodeDrag,
} from '@xyflow/react'
import { memo, useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'

import {
  useCreateCanvasEdge,
  useDeleteCanvasEdges,
  useUpdateCanvasCandidatePosition,
  useUpdateCanvasNodePositions,
} from '@/apis/canvas-apis'
import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import { flowEdgeTypes, flowNodeTypes } from '@/features/canvas/flownode/registry'
import {
  getContextNodePickerTargetNodeId,
  type ContextNodePickerTarget,
  useFlowNodeStore,
} from '@/features/canvas/flownode/store'
import type { CanvasFlowNode } from '@/features/canvas/flownode/types'
import { useSyncFlowNodeRenderDetail } from '@/features/canvas/flownode/use-sync-render-detail'
import {
  createArchivedNodeDragLayout,
  translateArchivedNodeDragLayout,
  type ArchivedNodeDragLayout,
} from '@/features/canvas/layout/layout-feature'
import { CanvasModeIndicator } from '@/features/canvas/surface/CanvasModeIndicator'
import { CanvasNodeCreationOverlay } from '@/features/canvas/surface/NodeCreationOverlay'
import { CanvasViewportControls } from '@/features/canvas/surface/ViewportControls'
import { canvasFitViewOptions, canvasMaxZoom, canvasMinZoom } from '@/features/canvas/surface/config'
import { useCanvasNodeCreation } from '@/features/canvas/surface/use-node-creation'
import { useCanvasSelection } from '@/features/canvas/surface/use-selection'
import { useArchiveLocks } from '@/features/canvas/story-spine/use-archive-locks'
import type { CanvasNode } from '@/types/canvas'
import type { ChapterArchiveVisibility } from '@/types/chapter-archive'

const canvasPanMouseButtons = [1, 2]
const reactFlowProOptions = { hideAttribution: true }

export const CanvasSurface = memo(function CanvasSurface({
  workId,
  canvasNodes,
  chapterArchives,
}: {
  workId: string
  canvasNodes: CanvasNode[]
  chapterArchives: ChapterArchiveVisibility[]
}) {
  const archiveLocks = useArchiveLocks(workId)
  const { mutate: updatePositions } = useUpdateCanvasNodePositions(workId)
  const { mutate: updateCandidatePosition } = useUpdateCanvasCandidatePosition(workId)
  const { mutate: deleteEdges } = useDeleteCanvasEdges(workId)
  const { mutate: createContextEdge } = useCreateCanvasEdge(workId)
  const nodes = useFlowNodeStore((state) => state.nodes)
  const selectedSourceNodeCount = useFlowNodeStore((state) => state.selectedSourceNodeIds.length)
  const edges = useFlowNodeStore((state) => state.edges)
  const canvasInteractionMode = useFlowNodeStore((state) => state.canvasInteractionMode)
  const contextNodePickerTarget = canvasInteractionMode.kind === 'context-node-picker'
    ? canvasInteractionMode.target
    : null
  const contextNodePickerTargetNodeId = getContextNodePickerTargetNodeId(canvasInteractionMode)
  const collaborativeContextNodeIds = useFlowNodeStore((state) => state.collaborativeContextNodeIds)
  const addCollaborativeContextNode = useFlowNodeStore((state) => state.actions.addCollaborativeContextNode)
  const cancelContextNodePicker = useFlowNodeStore((state) => state.actions.cancelContextNodePicker)
  const focusSourceNode = useFlowNodeStore((state) => state.actions.focusSourceNode)
  const showNodeToolbar = useFlowNodeStore((state) => state.actions.showNodeToolbar)
  const hideNodeToolbar = useFlowNodeStore((state) => state.actions.hideNodeToolbar)
  const openPreview = useFlowNodeStore((state) => state.actions.openPreview)
  const setNodePositions = useFlowNodeStore((state) => state.actions.setNodePositions)
  const dragLayoutRef = useRef<ArchivedNodeDragLayout | null>(null)
  const [contextPickReservations, setContextPickReservations] = useState<ReadonlySet<string>>(() => new Set())
  const { renderedNodes, onEdgesChange, onNodesChange, onSelectionEnd } = useCanvasSelection(nodes)
  const {
    dismiss: dismissCreationMenu,
    isConnecting,
    onConnect,
    onConnectEnd,
    onConnectStart,
    onMenuOpenChange,
    onPaneContextMenu,
    session: creationSession,
  } = useCanvasNodeCreation(workId)
  const isContextPicking = canvasInteractionMode.kind === 'context-node-picker'
  const collaborativeContextNodeIdSet = useMemo(
    () => new Set(collaborativeContextNodeIds),
    [collaborativeContextNodeIds],
  )

  const contextPickerConnectedNodeIds = useMemo(() => {
    const connectedNodeIds = new Set<string>()
    if (contextNodePickerTargetNodeId === null) return connectedNodeIds
    for (const edge of edges) {
      if (edge.data?.kind !== 'generated_from') continue
      if (edge.source === contextNodePickerTargetNodeId) connectedNodeIds.add(edge.target)
      if (edge.target === contextNodePickerTargetNodeId) connectedNodeIds.add(edge.source)
    }
    return connectedNodeIds
  }, [contextNodePickerTargetNodeId, edges])

  const contextPickerNodes = useMemo(() => {
    if (!isContextPicking) return renderedNodes
    return renderedNodes.map((node) => isContextNodeUnavailable(
      node,
      contextNodePickerTarget,
      contextPickerConnectedNodeIds,
      contextPickReservations,
      collaborativeContextNodeIdSet,
    )
      ? { ...node, className: appendNodeClassName(node.className, 'warmmo-flow__context-unavailable') }
      : node)
  }, [collaborativeContextNodeIdSet, contextNodePickerTarget, contextPickerConnectedNodeIds, contextPickReservations, isContextPicking, renderedNodes])

  useEffect(() => {
    if (!isContextPicking) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      cancelContextNodePicker()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [cancelContextNodePicker, isContextPicking])

  useEffect(() => {
    if (contextNodePickerTargetNodeId !== null) return
    setContextPickReservations((current) => current.size === 0 ? current : new Set())
  }, [contextNodePickerTargetNodeId])
  const deleteEdge = useCallback((edgeId: string, targetNodeId: string) => {
    if (
      !archiveLocks.isResolved
      || archiveLocks.lockedNodeIds.has(targetNodeId)
    ) return
    deleteEdges([edgeId])
  }, [archiveLocks.isResolved, archiveLocks.lockedNodeIds, deleteEdges])
  const renderedEdges = useMemo<CanvasFlowEdge[]>(() => edges.map((edge) => {
    const archiveLocked = archiveLocks.lockedNodeIds.has(edge.target)
    return {
      ...edge,
      type: 'canvas-edge',
      selectable: archiveLocked ? false : edge.selectable,
      deletable: archiveLocked ? false : edge.deletable,
      data: {
        ...edge.data,
        onDelete: archiveLocked ? undefined : () => deleteEdge(edge.id, edge.target),
      },
    } as CanvasFlowEdge
  }), [archiveLocks.lockedNodeIds, deleteEdge, edges])

  useSyncFlowNodeRenderDetail()

  const onNodeClick = useCallback<NodeMouseHandler<CanvasFlowNode>>((event, node) => {
    if (contextNodePickerTarget !== null) {
      event.preventDefault()
      event.stopPropagation()
      if (!isContextNodePickable(
        node,
        contextNodePickerTarget,
        contextPickerConnectedNodeIds,
        contextPickReservations,
        collaborativeContextNodeIdSet,
      )) return

      const contextNodeId = node.data.sourceId
      if (contextNodePickerTarget.kind === 'collaborative-agent') {
        addCollaborativeContextNode(contextNodeId)
        return
      }

      const targetNodeId = contextNodePickerTarget.nodeId
      const reservationKey = getContextPickReservationKey(targetNodeId, contextNodeId)
      setContextPickReservations((current) => new Set(current).add(reservationKey))
      createContextEdge({ sourceNodeId: contextNodeId, targetNodeId }, {
        onSuccess: () => {
          focusSourceNode(targetNodeId)
        },
        onError: () => {
          setContextPickReservations((current) => {
            if (!current.has(reservationKey)) return current
            const next = new Set(current)
            next.delete(reservationKey)
            return next
          })
        },
      })
      return
    }
    if (node.type === 'flow-node' && node.data.sourceType === 'node') showNodeToolbar(node.data.sourceId)
  }, [addCollaborativeContextNode, collaborativeContextNodeIdSet, contextNodePickerTarget, contextPickerConnectedNodeIds, contextPickReservations, createContextEdge, focusSourceNode, showNodeToolbar])

  const onNodeDragStart = useCallback<OnNodeDrag<CanvasFlowNode>>((_, node, draggedNodes) => {
    if (isContextPicking) return
    hideNodeToolbar()
    dragLayoutRef.current = draggedNodes.length > 1 || node.type !== 'flow-node' || node.data.sourceType !== 'node'
      ? null
      : createArchivedNodeDragLayout(node.data.sourceId, canvasNodes, chapterArchives)
  }, [canvasNodes, chapterArchives, hideNodeToolbar, isContextPicking])

  const onNodeDrag = useCallback<OnNodeDrag<CanvasFlowNode>>((_, node) => {
    const layout = dragLayoutRef.current
    if (layout === null || node.id !== layout.rootNodeId) return
    setNodePositions(translateArchivedNodeDragLayout(layout, node.position.x, node.position.y))
  }, [setNodePositions])

  const onNodeDragStop = useCallback<OnNodeDrag<CanvasFlowNode>>((_, node, draggedNodes) => {
    const layout = dragLayoutRef.current
    dragLayoutRef.current = null
    if (layout !== null && node.id === layout.rootNodeId) {
      const positions = translateArchivedNodeDragLayout(layout, node.position.x, node.position.y)
      setNodePositions(positions)
      updatePositions(positions)
      showNodeToolbar(layout.rootNodeId)
      return
    }
    const sourcePositions = draggedNodes.flatMap((draggedNode) =>
      draggedNode.type === 'flow-node' && draggedNode.data.sourceType === 'node'
        ? [{ nodeId: draggedNode.data.sourceId, x: draggedNode.position.x, y: draggedNode.position.y }]
        : [])
    if (sourcePositions.length > 0) updatePositions(sourcePositions)

    for (const draggedNode of draggedNodes) {
      if (draggedNode.type !== 'flow-node' || draggedNode.data.sourceType !== 'candidate') continue
      updateCandidatePosition({
        candidateId: draggedNode.data.sourceId,
        x: draggedNode.position.x,
        y: draggedNode.position.y,
      })
    }
    if (draggedNodes.length === 0 && node.type === 'flow-node' && node.data.sourceType === 'node') {
      updatePositions([{ nodeId: node.data.sourceId, x: node.position.x, y: node.position.y }])
    }
    if (node.type === 'flow-node' && node.data.sourceType === 'node') showNodeToolbar(node.data.sourceId)
  }, [setNodePositions, showNodeToolbar, updateCandidatePosition, updatePositions])

  const onNodeDoubleClick = useCallback<NodeMouseHandler<CanvasFlowNode>>((_, node) => {
    if (isContextPicking) return
    if (node.type === 'flow-node' && node.data.sourceType === 'node') openPreview(node.data.sourceId)
  }, [isContextPicking, openPreview])

  const handlePaneContextMenu = useCallback((event: ReactMouseEvent) => {
    if (isContextPicking) {
      event.preventDefault()
      return
    }
    onPaneContextMenu(event)
  }, [isContextPicking, onPaneContextMenu])

  return (
    <ReactFlow<CanvasFlowNode>
      nodes={contextPickerNodes}
      edges={renderedEdges}
      edgeTypes={flowEdgeTypes}
      nodeTypes={flowNodeTypes}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onConnect={onConnect}
      onConnectStart={onConnectStart}
      onConnectEnd={onConnectEnd}
      onNodeClick={onNodeClick}
      onNodeDrag={onNodeDrag}
      onNodeDragStart={onNodeDragStart}
      onNodeDragStop={onNodeDragStop}
      onNodeDoubleClick={onNodeDoubleClick}
      onPaneContextMenu={handlePaneContextMenu}
      onSelectionEnd={onSelectionEnd}
      deleteKeyCode={null}
      elementsSelectable={!isContextPicking}
      nodesConnectable={!isContextPicking}
      nodesDraggable={!isContextPicking}
      connectOnClick={false}
      connectionRadius={28}
      edgesReconnectable={false}
      fitViewOptions={canvasFitViewOptions}
      minZoom={canvasMinZoom}
      maxZoom={canvasMaxZoom}
      onlyRenderVisibleElements
      elevateNodesOnSelect
      selectionOnDrag={!isContextPicking}
      selectionMode={SelectionMode.Partial}
      panOnDrag={canvasPanMouseButtons}
      panOnScroll
      proOptions={reactFlowProOptions}
      className={`warmmo-flow ${selectedSourceNodeCount > 1 ? 'warmmo-flow--multi-selection' : ''} ${isConnecting ? 'warmmo-flow--connecting' : ''} ${isContextPicking ? 'warmmo-flow--context-picking' : ''}`}
    >
      <Background
        className="warmmo-flow__background"
        color="var(--color-hairline)"
        gap={20}
        size={1}
        variant={BackgroundVariant.Dots}
      />
      <CanvasModeIndicator mode={canvasInteractionMode} onExitContextPicker={cancelContextNodePicker} />
      <CanvasNodeCreationOverlay
        key={creationSession?.id ?? 'idle'}
        contextNodeIds={creationSession?.contextNodeIds ?? []}
        dropPoint={creationSession?.dropPoint ?? null}
        open={creationSession !== null}
        sourcePoint={creationSession?.sourcePoint ?? null}
        sourcePosition={creationSession?.sourcePosition ?? null}
        workId={workId}
        onDismiss={dismissCreationMenu}
        onOpenChange={onMenuOpenChange}
      />
      <CanvasViewportControls nodeCount={nodes.length} />
    </ReactFlow>
  )
})

function isContextNodePickable(
  node: CanvasFlowNode,
  target: ContextNodePickerTarget | null,
  connectedNodeIds: ReadonlySet<string>,
  reservations: ReadonlySet<string>,
  collaborativeContextNodeIds: ReadonlySet<string>,
) {
  if (target === null || node.type !== 'flow-node' || node.data.sourceType !== 'node') return false
  const nodeId = node.data.sourceId
  if (target.kind === 'collaborative-agent') return !collaborativeContextNodeIds.has(nodeId)
  const targetNodeId = target.nodeId
  return nodeId !== targetNodeId
    && !connectedNodeIds.has(nodeId)
    && !reservations.has(getContextPickReservationKey(targetNodeId, nodeId))
}

function isContextNodeUnavailable(
  node: CanvasFlowNode,
  target: ContextNodePickerTarget | null,
  connectedNodeIds: ReadonlySet<string>,
  reservations: ReadonlySet<string>,
  collaborativeContextNodeIds: ReadonlySet<string>,
) {
  if (target === null || node.type !== 'flow-node' || node.data.sourceType !== 'node') return false
  const nodeId = node.data.sourceId
  if (target.kind === 'collaborative-agent') return collaborativeContextNodeIds.has(nodeId)
  return nodeId === target.nodeId
    || connectedNodeIds.has(nodeId)
    || reservations.has(getContextPickReservationKey(target.nodeId, nodeId))
}

function appendNodeClassName(className: string | undefined, nextClassName: string) {
  return className === undefined || className === '' ? nextClassName : `${className} ${nextClassName}`
}

function getContextPickReservationKey(targetNodeId: string, nodeId: string) {
  return `${targetNodeId}\u0000${nodeId}`
}
