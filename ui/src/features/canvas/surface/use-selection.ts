import {
  useReactFlow,
  useStore,
  useStoreApi,
  type EdgeChange,
  type NodeChange,
  type SelectionRect,
} from '@xyflow/react'
import { useCallback, useEffect, useMemo, useRef, type MouseEvent as ReactMouseEvent } from 'react'

import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { CanvasFlowNode, SelectionProxyFlowNode, StoryFlowNode } from '@/features/canvas/flownode/types'

const selectionProxyId = '__warmnote-selection-proxy__'
const selectionFramePadding = 12
const fallbackNodeWidth = 256
const fallbackNodeHeight = 180

interface CanvasSelectionResult {
  renderedNodes: CanvasFlowNode[]
  onEdgesChange: (changes: EdgeChange<CanvasFlowEdge>[]) => void
  onNodesChange: (changes: NodeChange<CanvasFlowNode>[]) => void
  onSelectionEnd: (event: ReactMouseEvent) => void
}

export function useCanvasSelection(nodes: StoryFlowNode[]): CanvasSelectionResult {
  const flow = useReactFlow<CanvasFlowNode, CanvasFlowEdge>()
  const reactFlowStore = useStoreApi<CanvasFlowNode, CanvasFlowEdge>()
  const onNodesChange = useFlowNodeStore((state) => state.actions.onNodesChange)
  const onEdgesChange = useFlowNodeStore((state) => state.actions.onEdgesChange)
  const selectionRectRef = useRef<SelectionRect | null>(null)
  const selectionReady = useStore((state) => state.nodesSelectionActive && !state.userSelectionActive)
  const selectionProxyNode = useMemo(
    () => selectionReady ? createSelectionProxyNode(nodes) : null,
    [nodes, selectionReady],
  )
  const renderedNodes = useMemo<CanvasFlowNode[]>(
    () => selectionProxyNode === null ? nodes : [...nodes, selectionProxyNode],
    [nodes, selectionProxyNode],
  )

  useEffect(() => reactFlowStore.subscribe((state) => {
    if (state.userSelectionRect !== null) selectionRectRef.current = state.userSelectionRect
  }), [reactFlowStore])

  const handleNodesChange = useCallback((changes: NodeChange<CanvasFlowNode>[]) => {
    const storyNodeChanges = changes.filter(isStoryNodeChange)
    if (storyNodeChanges.length > 0) onNodesChange(storyNodeChanges)
  }, [onNodesChange])

  const handleEdgesChange = useCallback((changes: EdgeChange<CanvasFlowEdge>[]) => {
    onEdgesChange(changes)
  }, [onEdgesChange])

  const handleSelectionEnd = useCallback((event: ReactMouseEvent) => {
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

  return {
    renderedNodes,
    onEdgesChange: handleEdgesChange,
    onNodesChange: handleNodesChange,
    onSelectionEnd: handleSelectionEnd,
  }
}

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
  if (change.type === 'add' || change.type === 'replace') return change.item.type === 'flow-node'
  return change.id !== selectionProxyId
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
