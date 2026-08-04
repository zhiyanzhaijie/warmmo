import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  SelectionMode,
  useReactFlow,
  type NodeMouseHandler,
  type OnNodeDrag,
} from '@xyflow/react'
import { memo, useCallback, useEffect, useRef } from 'react'

import { useUpdateCanvasCandidatePosition, useUpdateCanvasNodePositions } from '@/apis/canvas-apis'
import { flowNodeTypes } from '@/features/canvas/flownode/registry'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'
import { useSyncFlowNodeRenderDetail } from '@/features/canvas/flownode/use-sync-render-detail'

const minimapNodeLimit = 2_000

export const CanvasSurface = memo(function CanvasSurface({ workId }: { workId: string }) {
  const flow = useReactFlow<StoryFlowNode>()
  const { mutate: updatePositions } = useUpdateCanvasNodePositions(workId)
  const { mutate: updateCandidatePosition } = useUpdateCanvasCandidatePosition(workId)
  const nodes = useFlowNodeStore((state) => state.nodes)
  const edges = useFlowNodeStore((state) => state.edges)
  const onNodesChange = useFlowNodeStore((state) => state.actions.onNodesChange)
  const onEdgesChange = useFlowNodeStore((state) => state.actions.onEdgesChange)
  const showNodeToolbar = useFlowNodeStore((state) => state.actions.showNodeToolbar)
  const hideNodeToolbar = useFlowNodeStore((state) => state.actions.hideNodeToolbar)
  const openPreview = useFlowNodeStore((state) => state.actions.openPreview)
  const fittedInitialNodesRef = useRef(false)

  useSyncFlowNodeRenderDetail()

  useEffect(() => {
    if (fittedInitialNodesRef.current || nodes.length === 0) return
    fittedInitialNodesRef.current = true
    requestAnimationFrame(() => void flow.fitView({ padding: 0.25, maxZoom: 1 }))
  }, [flow, nodes.length])

  const onNodeClick = useCallback<NodeMouseHandler<StoryFlowNode>>((_, node) => {
    if (node.data.sourceType === 'node') showNodeToolbar(node.data.sourceId)
  }, [showNodeToolbar])

  const onNodeDragStart = useCallback<OnNodeDrag<StoryFlowNode>>(() => {
    hideNodeToolbar()
  }, [hideNodeToolbar])

  const onNodeDragStop = useCallback<OnNodeDrag<StoryFlowNode>>((_, node, draggedNodes) => {
    const sourcePositions = draggedNodes.flatMap((draggedNode) =>
      draggedNode.data.sourceType === 'node'
        ? [{ nodeId: draggedNode.data.sourceId, x: draggedNode.position.x, y: draggedNode.position.y }]
        : [])
    if (sourcePositions.length > 0) updatePositions(sourcePositions)

    for (const draggedNode of draggedNodes) {
      if (draggedNode.data.sourceType !== 'candidate') continue
      updateCandidatePosition({
        candidateId: draggedNode.data.sourceId,
        x: draggedNode.position.x,
        y: draggedNode.position.y,
      })
    }
    if (draggedNodes.length === 0 && node.data.sourceType === 'node') {
      updatePositions([{ nodeId: node.data.sourceId, x: node.position.x, y: node.position.y }])
    }
    if (node.data.sourceType === 'node') showNodeToolbar(node.data.sourceId)
  }, [showNodeToolbar, updateCandidatePosition, updatePositions])

  const onNodeDoubleClick = useCallback<NodeMouseHandler<StoryFlowNode>>((_, node) => {
    if (node.data.sourceType === 'node') openPreview(node.data.sourceId)
  }, [openPreview])

  return (
    <ReactFlow<StoryFlowNode>
      nodes={nodes}
      edges={edges}
      nodeTypes={flowNodeTypes}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onNodeClick={onNodeClick}
      onNodeDragStart={onNodeDragStart}
      onNodeDragStop={onNodeDragStop}
      onNodeDoubleClick={onNodeDoubleClick}
      deleteKeyCode={null}
      nodesConnectable={false}
      edgesReconnectable={false}
      fitViewOptions={{ padding: 0.25, maxZoom: 1 }}
      minZoom={0.08}
      maxZoom={1.8}
      onlyRenderVisibleElements
      elevateNodesOnSelect
      selectionOnDrag
      selectionMode={SelectionMode.Partial}
      panOnScroll
      className="warmnote-flow"
    >
      <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="var(--color-hairline)" />
      <Controls position="bottom-right" showInteractive={false} />
      {nodes.length <= minimapNodeLimit ? (
        <MiniMap
          position="bottom-right"
          pannable
          zoomable
          className="!right-14 !h-24 !w-36 !rounded-sm !border !border-hairline !bg-canvas-elevated"
          nodeColor="var(--color-mute)"
          maskColor="color-mix(in srgb, var(--color-canvas) 76%, transparent)"
        />
      ) : null}
    </ReactFlow>
  )
})
