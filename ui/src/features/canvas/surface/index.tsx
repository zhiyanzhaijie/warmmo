import {
  Background,
  BackgroundVariant,
  ReactFlow,
  SelectionMode,
  type NodeMouseHandler,
  type OnNodeDrag,
} from '@xyflow/react'
import { memo, useCallback, useMemo, useRef } from 'react'

import { useDeleteCanvasEdges, useUpdateCanvasCandidatePosition, useUpdateCanvasNodePositions } from '@/apis/canvas-apis'
import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import { flowEdgeTypes, flowNodeTypes } from '@/features/canvas/flownode/registry'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { CanvasFlowNode } from '@/features/canvas/flownode/types'
import { useSyncFlowNodeRenderDetail } from '@/features/canvas/flownode/use-sync-render-detail'
import {
  createArchivedNodeDragLayout,
  translateArchivedNodeDragLayout,
  type ArchivedNodeDragLayout,
} from '@/features/canvas/layout/layout-feature'
import { CanvasNodeCreationOverlay } from '@/features/canvas/surface/NodeCreationOverlay'
import { CanvasViewportControls } from '@/features/canvas/surface/ViewportControls'
import { canvasFitViewOptions, canvasMaxZoom, canvasMinZoom } from '@/features/canvas/surface/config'
import { useCanvasNodeCreation } from '@/features/canvas/surface/use-node-creation'
import { useCanvasSelection } from '@/features/canvas/surface/use-selection'
import { useArchiveLocks } from '@/features/canvas/story-spine/use-archive-locks'
import type { CanvasNode } from '@/types/canvas'
import type { ChapterArchive } from '@/types/chapter-archive'

const canvasPanMouseButtons = [1, 2]
const reactFlowProOptions = { hideAttribution: true }

export const CanvasSurface = memo(function CanvasSurface({
  workId,
  canvasNodes,
  chapterArchives,
}: {
  workId: string
  canvasNodes: CanvasNode[]
  chapterArchives: ChapterArchive[]
}) {
  const archiveLocks = useArchiveLocks(workId)
  const { mutate: updatePositions } = useUpdateCanvasNodePositions(workId)
  const { mutate: updateCandidatePosition } = useUpdateCanvasCandidatePosition(workId)
  const { mutate: deleteEdges } = useDeleteCanvasEdges(workId)
  const nodes = useFlowNodeStore((state) => state.nodes)
  const selectedSourceNodeCount = useFlowNodeStore((state) => state.selectedSourceNodeIds.length)
  const edges = useFlowNodeStore((state) => state.edges)
  const showNodeToolbar = useFlowNodeStore((state) => state.actions.showNodeToolbar)
  const hideNodeToolbar = useFlowNodeStore((state) => state.actions.hideNodeToolbar)
  const openPreview = useFlowNodeStore((state) => state.actions.openPreview)
  const setNodePositions = useFlowNodeStore((state) => state.actions.setNodePositions)
  const dragLayoutRef = useRef<ArchivedNodeDragLayout | null>(null)
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

  const onNodeClick = useCallback<NodeMouseHandler<CanvasFlowNode>>((_, node) => {
    if (node.type === 'flow-node' && node.data.sourceType === 'node') showNodeToolbar(node.data.sourceId)
  }, [showNodeToolbar])

  const onNodeDragStart = useCallback<OnNodeDrag<CanvasFlowNode>>((_, node, draggedNodes) => {
    hideNodeToolbar()
    dragLayoutRef.current = draggedNodes.length > 1 || node.type !== 'flow-node' || node.data.sourceType !== 'node'
      ? null
      : createArchivedNodeDragLayout(node.data.sourceId, canvasNodes, chapterArchives)
  }, [canvasNodes, chapterArchives, hideNodeToolbar])

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
    if (node.type === 'flow-node' && node.data.sourceType === 'node') openPreview(node.data.sourceId)
  }, [openPreview])

  return (
    <ReactFlow<CanvasFlowNode>
      nodes={renderedNodes}
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
      onPaneContextMenu={onPaneContextMenu}
      onSelectionEnd={onSelectionEnd}
      deleteKeyCode={null}
      nodesConnectable
      connectOnClick={false}
      connectionRadius={28}
      edgesReconnectable={false}
      fitViewOptions={canvasFitViewOptions}
      minZoom={canvasMinZoom}
      maxZoom={canvasMaxZoom}
      onlyRenderVisibleElements
      elevateNodesOnSelect
      selectionOnDrag
      selectionMode={SelectionMode.Partial}
      panOnDrag={canvasPanMouseButtons}
      panOnScroll
      proOptions={reactFlowProOptions}
      className={`warmnote-flow ${selectedSourceNodeCount > 1 ? 'warmnote-flow--multi-selection' : ''} ${isConnecting ? 'warmnote-flow--connecting' : ''}`}
    >
      <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="var(--color-hairline)" />
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
