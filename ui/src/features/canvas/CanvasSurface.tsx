import {
  Background,
  BackgroundVariant,
  MiniMap,
  ReactFlow,
  SelectionMode,
  useReactFlow,
  useStore,
  type NodeMouseHandler,
  type OnNodeDrag,
} from '@xyflow/react'
import { Focus, Map as MapIcon, ZoomIn, ZoomOut } from 'lucide-react'
import { memo, useCallback, useEffect, useRef, useState } from 'react'

import { useUpdateCanvasCandidatePosition, useUpdateCanvasNodePositions } from '@/apis/canvas-apis'
import { flowNodeTypes } from '@/features/canvas/flownode/registry'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'
import { isTextEntryTarget } from '@/features/canvas/keyboard'
import { useSyncFlowNodeRenderDetail } from '@/features/canvas/flownode/use-sync-render-detail'

const minimapNodeLimit = 2_000
const canvasMinZoom = 0.08
const canvasMaxZoom = 1.8
const canvasPanMouseButtons = [1, 2]
const reactFlowProOptions = { hideAttribution: true }
const canvasFitViewOptions = { padding: 0.25, maxZoom: 1 }
const minimapStyle = {
  width: 144,
  height: 96,
  left: 14,
  bottom: 58,
  margin: 0,
}

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
  const [isMinimapVisible, setIsMinimapVisible] = useState(true)
  const isMinimapAvailable = nodes.length <= minimapNodeLimit

  useSyncFlowNodeRenderDetail()

  const toggleMinimap = useCallback(() => {
    if (isMinimapAvailable) setIsMinimapVisible((visible) => !visible)
  }, [isMinimapAvailable])

  const zoomIn = useCallback(() => {
    void flow.zoomIn({ duration: 160 })
  }, [flow])

  const zoomOut = useCallback(() => {
    void flow.zoomOut({ duration: 160 })
  }, [flow])

  const fitCanvas = useCallback(() => {
    void flow.fitView({ ...canvasFitViewOptions, duration: 200 })
  }, [flow])

  useEffect(() => {
    if (fittedInitialNodesRef.current || nodes.length === 0) return
    fittedInitialNodesRef.current = true
    requestAnimationFrame(() => void flow.fitView({ padding: 0.25, maxZoom: 1 }))
  }, [flow, nodes.length])

  useEffect(() => {
    const handleViewportShortcut = (event: KeyboardEvent) => {
      if (
        event.repeat
        || event.altKey
        || isTextEntryTarget(event.target)
      ) return

      const commandModifier = event.ctrlKey || event.metaKey
      if (!commandModifier) return

      if (event.code === 'Equal' || event.code === 'NumpadAdd') {
        event.preventDefault()
        zoomIn()
        return
      }
      if (event.code === 'Minus' || event.code === 'NumpadSubtract') {
        event.preventDefault()
        zoomOut()
        return
      }
      if (event.code === 'KeyF') {
        event.preventDefault()
        fitCanvas()
        return
      }
      if (event.code === 'KeyM' && isMinimapAvailable) {
        event.preventDefault()
        toggleMinimap()
      }
    }

    window.addEventListener('keydown', handleViewportShortcut)
    return () => window.removeEventListener('keydown', handleViewportShortcut)
  }, [fitCanvas, isMinimapAvailable, toggleMinimap, zoomIn, zoomOut])

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
      className="warmnote-flow"
    >
      <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="var(--color-hairline)" />
      <CanvasViewportToolbar
        isMinimapAvailable={isMinimapAvailable}
        isMinimapVisible={isMinimapVisible}
        onFitCanvas={fitCanvas}
        onToggleMinimap={toggleMinimap}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
      />
      {isMinimapAvailable && isMinimapVisible ? (
        <MiniMap<StoryFlowNode>
          ariaLabel="画布缩略图"
          bgColor="color-mix(in srgb, var(--color-ink) 9%, var(--color-canvas))"
          className="warmnote-flow__minimap"
          maskColor="color-mix(in srgb, var(--color-canvas) 76%, transparent)"
          nodeBorderRadius={2}
          nodeColor="color-mix(in srgb, var(--color-ink) 58%, var(--color-canvas))"
          nodeStrokeColor="transparent"
          position="bottom-left"
          pannable
          style={minimapStyle}
          zoomable
        />
      ) : null}
    </ReactFlow>
  )
})

const CanvasViewportToolbar = memo(function CanvasViewportToolbar({
  isMinimapAvailable,
  isMinimapVisible,
  onFitCanvas,
  onToggleMinimap,
  onZoomIn,
  onZoomOut,
}: {
  isMinimapAvailable: boolean
  isMinimapVisible: boolean
  onFitCanvas: () => void
  onToggleMinimap: () => void
  onZoomIn: () => void
  onZoomOut: () => void
}) {
  const zoom = useStore((state) => state.transform[2])

  return (
    <div
      aria-label="画布视图控制"
      className="warmnote-flow__viewport-toolbar nodrag nopan nowheel"
      role="toolbar"
    >
      <button
        aria-label="放大"
        className="warmnote-flow__viewport-button"
        disabled={zoom >= canvasMaxZoom}
        title="放大 · Ctrl/⌘ +"
        type="button"
        onClick={onZoomIn}
      >
        <ZoomIn size={15} aria-hidden="true" />
      </button>
      <button
        aria-label="缩小"
        className="warmnote-flow__viewport-button"
        disabled={zoom <= canvasMinZoom}
        title="缩小 · Ctrl/⌘ -"
        type="button"
        onClick={onZoomOut}
      >
        <ZoomOut size={15} aria-hidden="true" />
      </button>
      <button
        aria-label="适应画布"
        className="warmnote-flow__viewport-button"
        title="适应画布 · Ctrl/⌘ F"
        type="button"
        onClick={onFitCanvas}
      >
        <Focus size={15} aria-hidden="true" />
      </button>
      <button
        aria-label={isMinimapVisible ? '关闭小地图' : '打开小地图'}
        aria-pressed={isMinimapVisible && isMinimapAvailable}
        className={`warmnote-flow__viewport-button ${isMinimapVisible && isMinimapAvailable ? 'warmnote-flow__viewport-button--active' : ''}`}
        disabled={!isMinimapAvailable}
        title={isMinimapAvailable ? '切换小地图 · Ctrl/⌘ M' : '节点过多，无法显示小地图'}
        type="button"
        onClick={onToggleMinimap}
      >
        <MapIcon size={15} aria-hidden="true" />
      </button>
    </div>
  )
})
