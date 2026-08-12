import {
  Position,
  useReactFlow,
  type Connection,
  type OnConnectEnd,
  type XYPosition,
} from '@xyflow/react'
import { useCallback, useState, type MouseEvent as ReactMouseEvent } from 'react'

import { useCreateCanvasEdge } from '@/apis/canvas-apis'
import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import type { CanvasFlowNode } from '@/features/canvas/flownode/types'
import type { NodeCreationSession } from '@/features/canvas/surface/types'

type OpenNodeCreationRequest = Omit<NodeCreationSession, 'id'>

export function useCanvasNodeCreation(workId: string) {
  const flow = useReactFlow<CanvasFlowNode, CanvasFlowEdge>()
  const { mutate: createEdge } = useCreateCanvasEdge(workId)
  const [session, setSession] = useState<NodeCreationSession | null>(null)
  const [isConnecting, setIsConnecting] = useState(false)

  const dismiss = useCallback(() => {
    setSession(null)
  }, [])

  const onMenuOpenChange = useCallback((open: boolean) => {
    if (!open) dismiss()
  }, [dismiss])

  const open = useCallback((request: OpenNodeCreationRequest) => {
    setSession((current) => ({ ...request, id: (current?.id ?? 0) + 1 }))
  }, [])

  const onConnectStart = useCallback(() => {
    dismiss()
    setIsConnecting(true)
  }, [dismiss])

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
    if (
      contextNodeIds.length === 0
      || screenPoint === null
      || (sourceNode.type === 'flow-node' && !sourceNode.data.archiveStateResolved)
    ) return

    open({
      contextNodeIds,
      dropPoint: flow.screenToFlowPosition(screenPoint),
      sourcePoint: connectionState.from,
      sourcePosition: connectionState.fromPosition,
    })
  }, [flow, open])

  const onConnect = useCallback((connection: Connection) => {
    const sourceNode = flow.getNode(connection.source)
    const targetNode = flow.getNode(connection.target)
    if (
      sourceNode?.type !== 'flow-node'
      || targetNode?.type !== 'flow-node'
      || sourceNode.data.sourceType !== 'node'
      || targetNode.data.sourceType !== 'node'
      || !sourceNode.data.archiveStateResolved
      || !targetNode.data.archiveStateResolved
      || targetNode.data.archiveLocked
    ) return

    createEdge({
      sourceNodeId: sourceNode.data.sourceId,
      targetNodeId: targetNode.data.sourceId,
    })
  }, [createEdge, flow])

  const onPaneContextMenu = useCallback((event: ReactMouseEvent) => {
    event.preventDefault()
    open({
      contextNodeIds: [],
      dropPoint: flow.screenToFlowPosition({ x: event.clientX, y: event.clientY }),
      sourcePoint: null,
      sourcePosition: null,
    })
  }, [flow, open])

  return {
    dismiss,
    isConnecting,
    onConnect,
    onConnectEnd,
    onConnectStart,
    onMenuOpenChange,
    onPaneContextMenu,
    session,
  }
}

function getConnectionEndScreenPoint(event: MouseEvent | TouchEvent): XYPosition | null {
  if ('changedTouches' in event) {
    const touch = event.changedTouches[0]
    return touch === undefined ? null : { x: touch.clientX, y: touch.clientY }
  }
  return { x: event.clientX, y: event.clientY }
}
