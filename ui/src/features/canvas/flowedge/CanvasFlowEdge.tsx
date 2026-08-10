import {
  BaseEdge,
  EdgeToolbar,
  getBezierPath,
  useReactFlow,
  type EdgeProps,
} from '@xyflow/react'
import { Scissors } from 'lucide-react'
import { memo, useEffect, useRef, useState, type MouseEvent } from 'react'

import type { CanvasFlowEdge as CanvasFlowEdgeType } from '@/features/canvas/flowedge/types'

export const CanvasFlowEdge = memo(function CanvasFlowEdge({
  data,
  id,
  markerEnd,
  markerStart,
  selected,
  sourcePosition,
  sourceX,
  sourceY,
  style,
  targetPosition,
  targetX,
  targetY,
}: EdgeProps<CanvasFlowEdgeType>) {
  const { screenToFlowPosition } = useReactFlow()
  const [hovered, setHovered] = useState(false)
  const [actionPosition, setActionPosition] = useState({ x: 0, y: 0 })
  const hideTimerRef = useRef<number | null>(null)
  const [path] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })
  const canDelete = data?.persisted === true && data.onDelete !== undefined

  const updateActionPosition = (event: MouseEvent<SVGGElement>) => {
    const position = screenToFlowPosition({ x: event.clientX, y: event.clientY })
    setActionPosition((currentPosition) =>
      currentPosition.x === position.x && currentPosition.y === position.y
        ? currentPosition
        : position)
  }

  const showActions = () => {
    if (hideTimerRef.current !== null) window.clearTimeout(hideTimerRef.current)
    hideTimerRef.current = null
    setHovered(true)
  }

  const handleEdgeMouseEnter = (event: MouseEvent<SVGGElement>) => {
    updateActionPosition(event)
    showActions()
  }

  const handleEdgeMouseMove = (event: MouseEvent<SVGGElement>) => {
    updateActionPosition(event)
  }

  const scheduleHideActions = () => {
    if (hideTimerRef.current !== null) window.clearTimeout(hideTimerRef.current)
    hideTimerRef.current = window.setTimeout(() => {
      setHovered(false)
      hideTimerRef.current = null
    }, 180)
  }

  useEffect(() => () => {
    if (hideTimerRef.current !== null) window.clearTimeout(hideTimerRef.current)
  }, [])

  return (
    <>
      <g
        onMouseEnter={handleEdgeMouseEnter}
        onMouseLeave={scheduleHideActions}
        onMouseMove={handleEdgeMouseMove}
      >
        <BaseEdge
          id={id}
          className="warmmo-flow__edge-path"
          data-edge-id={id}
          interactionWidth={28}
          markerEnd={markerEnd}
          markerStart={markerStart}
          path={path}
          style={{
            ...style,
            stroke: selected ? 'var(--color-link)' : style?.stroke,
            strokeWidth: selected ? 2 : style?.strokeWidth,
          }}
        />
      </g>
      {canDelete ? (
        <EdgeToolbar
          edgeId={id}
          isVisible={hovered}
          x={actionPosition.x}
          y={actionPosition.y}
          onMouseEnter={showActions}
          onMouseLeave={scheduleHideActions}
        >
          <button
            aria-label="删除连接"
            className="warmmo-flow__edge-cut nodrag nopan nowheel"
            title="删除连接"
            type="button"
            onClick={(event) => {
              event.stopPropagation()
              data.onDelete?.(id)
            }}
          >
            <Scissors aria-hidden="true" size={13} />
          </button>
        </EdgeToolbar>
      ) : null}
    </>
  )
})
