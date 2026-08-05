import {
  BaseEdge,
  EdgeToolbar,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react'
import { Scissors } from 'lucide-react'
import { memo, useEffect, useRef, useState } from 'react'

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
  const [hovered, setHovered] = useState(false)
  const hideTimerRef = useRef<number | null>(null)
  const [path, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  })
  const canDelete = data?.persisted === true && data.onDelete !== undefined

  const showActions = () => {
    if (hideTimerRef.current !== null) window.clearTimeout(hideTimerRef.current)
    hideTimerRef.current = null
    setHovered(true)
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
      <g onMouseEnter={showActions} onMouseLeave={scheduleHideActions}>
        <BaseEdge
          id={id}
          className="warmnote-flow__edge-path"
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
          isVisible={hovered || selected}
          x={labelX}
          y={labelY}
          onMouseEnter={showActions}
          onMouseLeave={scheduleHideActions}
        >
          <button
            aria-label="删除连接"
            className="warmnote-flow__edge-cut nodrag nopan nowheel"
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
