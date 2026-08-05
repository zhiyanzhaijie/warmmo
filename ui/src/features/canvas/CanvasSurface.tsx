import {
  Background,
  BackgroundVariant,
  MiniMap,
  Position,
  ReactFlow,
  SelectionMode,
  useReactFlow,
  useStore,
  useStoreApi,
  type Connection,
  type EdgeChange,
  type NodeChange,
  type NodeMouseHandler,
  type OnConnectEnd,
  type OnNodeDrag,
  type SelectionRect,
  type XYPosition,
} from '@xyflow/react'
import { Focus, Map as MapIcon, ZoomIn, ZoomOut } from 'lucide-react'
import { memo, useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'

import { useCreateCanvasEdge, useDeleteCanvasEdges, useUpdateCanvasCandidatePosition, useUpdateCanvasNodePositions } from '@/apis/canvas-apis'
import { CanvasSelectionComposerHandle } from '@/features/canvas/CanvasSelectionComposerHandle'
import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import { flowEdgeTypes, flowNodeTypes } from '@/features/canvas/flownode/registry'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { CanvasFlowNode, SelectionProxyFlowNode, StoryFlowNode } from '@/features/canvas/flownode/types'
import { isTextEntryTarget } from '@/features/canvas/keyboard'
import { useSyncFlowNodeRenderDetail } from '@/features/canvas/flownode/use-sync-render-detail'

const minimapNodeLimit = 2_000
const canvasMinZoom = 0.08
const canvasMaxZoom = 1.8
const canvasPanMouseButtons = [1, 2]
const reactFlowProOptions = { hideAttribution: true }
const canvasFitViewOptions = { padding: 0.25, maxZoom: 1 }
const selectionProxyId = '__warmnote-selection-proxy__'
const selectionFramePadding = 12
const fallbackNodeWidth = 256
const fallbackNodeHeight = 180
const minimapStyle = {
  width: 144,
  height: 96,
  left: 14,
  bottom: 58,
  margin: 0,
}

export const CanvasSurface = memo(function CanvasSurface({ workId }: { workId: string }) {
  const flow = useReactFlow<CanvasFlowNode, CanvasFlowEdge>()
  const reactFlowStore = useStoreApi<CanvasFlowNode, CanvasFlowEdge>()
  const { mutate: updatePositions } = useUpdateCanvasNodePositions(workId)
  const { mutate: updateCandidatePosition } = useUpdateCanvasCandidatePosition(workId)
  const { mutate: createEdge } = useCreateCanvasEdge(workId)
  const { mutate: deleteEdges } = useDeleteCanvasEdges(workId)
  const nodes = useFlowNodeStore((state) => state.nodes)
  const selectedSourceNodeCount = useFlowNodeStore((state) => state.selectedSourceNodeIds.length)
  const edges = useFlowNodeStore((state) => state.edges)
  const onNodesChange = useFlowNodeStore((state) => state.actions.onNodesChange)
  const onEdgesChange = useFlowNodeStore((state) => state.actions.onEdgesChange)
  const showNodeToolbar = useFlowNodeStore((state) => state.actions.showNodeToolbar)
  const hideNodeToolbar = useFlowNodeStore((state) => state.actions.hideNodeToolbar)
  const openPreview = useFlowNodeStore((state) => state.actions.openPreview)
  const fittedInitialNodesRef = useRef(false)
  const selectionRectRef = useRef<SelectionRect | null>(null)
  const [isMinimapVisible, setIsMinimapVisible] = useState(true)
  const [composerContextNodeIds, setComposerContextNodeIds] = useState<string[]>([])
  const [composerDropPoint, setComposerDropPoint] = useState<XYPosition | null>(null)
  const [composerSourcePoint, setComposerSourcePoint] = useState<XYPosition | null>(null)
  const [composerSourcePosition, setComposerSourcePosition] = useState<Position | null>(null)
  const [isComposerMenuOpen, setIsComposerMenuOpen] = useState(false)
  const [isConnecting, setIsConnecting] = useState(false)
  const isMinimapAvailable = nodes.length <= minimapNodeLimit
  const selectionReady = useStore((state) => state.nodesSelectionActive && !state.userSelectionActive)
  const selectionProxyNode = useMemo(
    () => selectionReady ? createSelectionProxyNode(nodes) : null,
    [nodes, selectionReady],
  )
  const renderedNodes = useMemo<CanvasFlowNode[]>(
    () => selectionProxyNode === null ? nodes : [...nodes, selectionProxyNode],
    [nodes, selectionProxyNode],
  )
  const deleteEdge = useCallback((edgeId: string) => {
    deleteEdges([edgeId])
  }, [deleteEdges])
  const renderedEdges = useMemo<CanvasFlowEdge[]>(() => edges.map((edge) => ({
    ...edge,
    type: 'canvas-edge',
    data: { ...edge.data, onDelete: deleteEdge },
  } as CanvasFlowEdge)), [deleteEdge, edges])

  useSyncFlowNodeRenderDetail()

  useEffect(() => reactFlowStore.subscribe((state) => {
    if (state.userSelectionRect !== null) selectionRectRef.current = state.userSelectionRect
  }), [reactFlowStore])

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

  const onNodeClick = useCallback<NodeMouseHandler<CanvasFlowNode>>((_, node) => {
    if (node.type === 'flow-node' && node.data.sourceType === 'node') showNodeToolbar(node.data.sourceId)
  }, [showNodeToolbar])

  const onNodeDragStart = useCallback<OnNodeDrag<CanvasFlowNode>>(() => {
    hideNodeToolbar()
  }, [hideNodeToolbar])

  const onNodeDragStop = useCallback<OnNodeDrag<CanvasFlowNode>>((_, node, draggedNodes) => {
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
  }, [showNodeToolbar, updateCandidatePosition, updatePositions])

  const onNodeDoubleClick = useCallback<NodeMouseHandler<CanvasFlowNode>>((_, node) => {
    if (node.type === 'flow-node' && node.data.sourceType === 'node') openPreview(node.data.sourceId)
  }, [openPreview])

  const onCanvasNodesChange = useCallback((changes: NodeChange<CanvasFlowNode>[]) => {
    const storyNodeChanges = changes.filter(isStoryNodeChange)
    if (storyNodeChanges.length > 0) onNodesChange(storyNodeChanges)
  }, [onNodesChange])

  const onCanvasEdgesChange = useCallback((changes: EdgeChange<CanvasFlowEdge>[]) => {
    onEdgesChange(changes)
  }, [onEdgesChange])

  const onSelectionEnd = useCallback((event: ReactMouseEvent) => {
    const selectionRect = selectionRectRef.current
    selectionRectRef.current = null
    if (selectionRect === null) return
    const paneBounds = event.currentTarget.getBoundingClientRect()
    const selectedEdgeIds = collectEdgesIntersectingSelection({
      left: paneBounds.left + selectionRect.x,
      top: paneBounds.top + selectionRect.y,
      right: paneBounds.left + selectionRect.x + selectionRect.width,
      bottom: paneBounds.top + selectionRect.y + selectionRect.height,
    })
    const changes = flow.getEdges().flatMap((edge) => edge.selectable === false
      ? []
      : [{ id: edge.id, type: 'select' as const, selected: selectedEdgeIds.has(edge.id) }])
    if (changes.length > 0) onEdgesChange(changes)
  }, [flow, onEdgesChange])

  const onConnectEnd = useCallback<OnConnectEnd<CanvasFlowNode>>((event, connectionState) => {
    setIsConnecting(false)
    if (connectionState.fromNode === null || connectionState.toNode !== null) return
    const sourceNode = connectionState.fromNode.internals.userNode
    const contextNodeIds = sourceNode.type === 'selection-proxy'
      ? sourceNode.data.contextNodeIds
      : sourceNode.data.sourceType === 'node'
        ? [sourceNode.data.sourceId]
        : []
    const screenPoint = getConnectionEndScreenPoint(event)
    if (contextNodeIds.length === 0 || screenPoint === null) return

    setComposerContextNodeIds(contextNodeIds)
    setComposerSourcePoint(connectionState.from)
    setComposerSourcePosition(connectionState.fromPosition)
    setComposerDropPoint(flow.screenToFlowPosition(screenPoint))
    setIsComposerMenuOpen(true)
  }, [flow])

  const onComposerOpenChange = useCallback((open: boolean) => {
    setIsComposerMenuOpen(open)
  }, [])

  const dismissComposer = useCallback(() => {
    setIsComposerMenuOpen(false)
    setComposerContextNodeIds([])
    setComposerDropPoint(null)
    setComposerSourcePoint(null)
    setComposerSourcePosition(null)
  }, [])

  const onConnectStart = useCallback(() => {
    dismissComposer()
    setIsConnecting(true)
  }, [dismissComposer])

  const onConnect = useCallback((connection: Connection) => {
    const sourceNode = flow.getNode(connection.source)
    const targetNode = flow.getNode(connection.target)
    if (
      sourceNode?.type !== 'flow-node'
      || targetNode?.type !== 'flow-node'
      || sourceNode.data.sourceType !== 'node'
      || targetNode.data.sourceType !== 'node'
    ) return
    createEdge({
      sourceNodeId: sourceNode.data.sourceId,
      targetNodeId: targetNode.data.sourceId,
    })
  }, [createEdge, flow])

  return (
    <ReactFlow<CanvasFlowNode>
      nodes={renderedNodes}
      edges={renderedEdges}
      edgeTypes={flowEdgeTypes}
      nodeTypes={flowNodeTypes}
      onNodesChange={onCanvasNodesChange}
      onEdgesChange={onCanvasEdgesChange}
      onConnect={onConnect}
      onConnectStart={onConnectStart}
      onConnectEnd={onConnectEnd}
      onNodeClick={onNodeClick}
      onNodeDragStart={onNodeDragStart}
      onNodeDragStop={onNodeDragStop}
      onNodeDoubleClick={onNodeDoubleClick}
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
      <CanvasSelectionComposerHandle
        contextNodeIds={composerContextNodeIds}
        dropPoint={composerDropPoint}
        open={isComposerMenuOpen}
        sourcePoint={composerSourcePoint}
        sourcePosition={composerSourcePosition}
        workId={workId}
        onDismiss={dismissComposer}
        onOpenChange={onComposerOpenChange}
      />
      <CanvasViewportToolbar
        isMinimapAvailable={isMinimapAvailable}
        isMinimapVisible={isMinimapVisible}
        onFitCanvas={fitCanvas}
        onToggleMinimap={toggleMinimap}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
      />
      {isMinimapAvailable && isMinimapVisible ? (
        <MiniMap<CanvasFlowNode>
          ariaLabel="画布缩略图"
          bgColor="color-mix(in srgb, var(--color-ink) 9%, var(--color-canvas))"
          className="warmnote-flow__minimap"
          maskColor="color-mix(in srgb, var(--color-canvas) 76%, transparent)"
          nodeBorderRadius={2}
          nodeClassName={getMinimapNodeClassName}
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

function createSelectionProxyNode(nodes: StoryFlowNode[]): SelectionProxyFlowNode | null {
  const selectedNodes = nodes.filter((node) => node.selected && node.data.sourceType === 'node')
  if (selectedNodes.length < 2) return null

  let left = Number.POSITIVE_INFINITY
  let top = Number.POSITIVE_INFINITY
  let right = Number.NEGATIVE_INFINITY
  let bottom = Number.NEGATIVE_INFINITY
  for (const node of selectedNodes) {
    const width = node.measured?.width ?? node.width ?? fallbackNodeWidth
    const height = node.measured?.height ?? node.height ?? fallbackNodeHeight
    left = Math.min(left, node.position.x)
    top = Math.min(top, node.position.y)
    right = Math.max(right, node.position.x + width)
    bottom = Math.max(bottom, node.position.y + height)
  }

  return {
    id: selectionProxyId,
    type: 'selection-proxy',
    position: {
      x: left - selectionFramePadding,
      y: top - selectionFramePadding,
    },
    data: {
      contextNodeIds: selectedNodes.map((node) => node.data.sourceId),
    },
    style: {
      width: right - left + selectionFramePadding * 2,
      height: bottom - top + selectionFramePadding * 2,
      pointerEvents: 'none',
    },
    draggable: false,
    selectable: false,
    deletable: false,
    focusable: false,
    zIndex: 1_000,
  }
}

function isStoryNodeChange(change: NodeChange<CanvasFlowNode>): change is NodeChange<StoryFlowNode> {
  if (change.type === 'add' || change.type === 'replace') {
    return change.item.type === 'flow-node'
  }
  return change.id !== selectionProxyId
}

function getMinimapNodeClassName(node: CanvasFlowNode) {
  return node.type === 'selection-proxy' ? 'warmnote-flow__minimap-selection-proxy' : ''
}

function getConnectionEndScreenPoint(event: MouseEvent | TouchEvent): XYPosition | null {
  if ('changedTouches' in event) {
    const touch = event.changedTouches[0]
    return touch === undefined ? null : { x: touch.clientX, y: touch.clientY }
  }
  return { x: event.clientX, y: event.clientY }
}

interface ScreenRect {
  left: number
  top: number
  right: number
  bottom: number
}

function collectEdgesIntersectingSelection(selection: ScreenRect) {
  const selectedEdgeIds = new Set<string>()
  const paths = document.querySelectorAll<SVGPathElement>('.warmnote-flow__edge-path[data-edge-id]')
  for (const path of paths) {
    const edgeId = path.dataset.edgeId
    const matrix = path.getScreenCTM()
    if (edgeId === undefined || matrix === null || !rectsIntersect(path.getBoundingClientRect(), selection)) continue
    const length = path.getTotalLength()
    const screenScale = Math.hypot(matrix.a, matrix.b)
    const sampleCount = Math.min(256, Math.max(8, Math.ceil(length * screenScale / 6)))
    for (let index = 0; index <= sampleCount; index += 1) {
      const point = path.getPointAtLength(length * index / sampleCount).matrixTransform(matrix)
      if (point.x >= selection.left && point.x <= selection.right && point.y >= selection.top && point.y <= selection.bottom) {
        selectedEdgeIds.add(edgeId)
        break
      }
    }
  }
  return selectedEdgeIds
}

function rectsIntersect(left: DOMRect, right: ScreenRect) {
  return left.right >= right.left
    && left.left <= right.right
    && left.bottom >= right.top
    && left.top <= right.bottom
}

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
