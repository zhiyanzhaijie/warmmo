import { Position, ViewportPortal, getBezierPath, type XYPosition } from '@xyflow/react'
import { memo, useCallback, useRef } from 'react'

import { useCreateCanvasNode } from '@/apis/canvas-apis'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { nodeDefinitions, type CanvasNodeKind } from '@/features/canvas/nodes/definitions'
import { CanvasNodeCommandMenu } from '@/features/canvas/surface/NodeCommandMenu'

const fallbackNodeWidth = 256
const fallbackNodeHeight = 180

export const CanvasNodeCreationOverlay = memo(function CanvasNodeCreationOverlay({
  contextNodeIds,
  dropPoint,
  open,
  sourcePoint,
  sourcePosition,
  workId,
  onDismiss,
  onOpenChange,
}: {
  contextNodeIds: string[]
  dropPoint: XYPosition | null
  open: boolean
  sourcePoint: XYPosition | null
  sourcePosition: Position | null
  workId: string
  onDismiss: () => void
  onOpenChange: (open: boolean) => void
}) {
  const createNode = useCreateCanvasNode(workId)
  const focusSourceNode = useFlowNodeStore((state) => state.actions.focusSourceNode)
  const creatingRef = useRef(false)

  const createAtDropPoint = useCallback((kind: CanvasNodeKind) => {
    if (dropPoint === null || createNode.isPending) return
    const definition = nodeDefinitions[kind]
    creatingRef.current = true
    createNode.mutate({
      kind,
      title: `未命名${definition.label}`,
      content: '',
      x: dropPoint.x - fallbackNodeWidth / 2,
      y: dropPoint.y - fallbackNodeHeight / 2,
      ...(contextNodeIds.length > 0 ? { contextNodeIds } : {}),
    }, {
      onSuccess: (node) => {
        focusSourceNode(node.id)
        creatingRef.current = false
        onDismiss()
      },
      onError: () => {
        creatingRef.current = false
        onDismiss()
      },
    })
  }, [contextNodeIds, createNode, dropPoint, focusSourceNode, onDismiss])

  if (dropPoint === null) return null

  const temporaryPath = sourcePoint !== null && sourcePosition !== null
    ? getBezierPath({
        sourceX: sourcePoint.x,
        sourceY: sourcePoint.y,
        sourcePosition,
        targetX: dropPoint.x,
        targetY: dropPoint.y,
        targetPosition: sourcePosition === Position.Right ? Position.Left : Position.Right,
      })[0]
    : null
  const menuLabel = contextNodeIds.length === 0
    ? '创建节点'
    : contextNodeIds.length === 1
      ? '引用该节点生成'
      : '引用所选节点生成'

  return (
    <ViewportPortal>
      {temporaryPath !== null ? (
        <svg
          aria-hidden="true"
          className="pointer-events-none absolute left-0 top-0 z-20 overflow-visible"
          width="1"
          height="1"
        >
          <path
            className="warmnote-flow__temporary-edge"
            d={temporaryPath}
            fill="none"
            vectorEffect="non-scaling-stroke"
          />
        </svg>
      ) : null}
      <div
        className="nodrag nopan nowheel absolute z-30 -translate-x-1/2 -translate-y-1/2"
        style={{ left: dropPoint.x, top: dropPoint.y }}
      >
        <CanvasNodeCommandMenu
          label={menuLabel}
          open={open}
          disabled={createNode.isPending}
          onOpenChange={(nextOpen) => {
            onOpenChange(nextOpen)
            if (!nextOpen && !creatingRef.current) onDismiss()
          }}
          onSelect={createAtDropPoint}
        />
      </div>
    </ViewportPortal>
  )
})
