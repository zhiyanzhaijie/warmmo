import { Handle, Position, useNodeId, useUpdateNodeInternals } from '@xyflow/react'
import { memo, useCallback, useEffect, useRef, useState, type PointerEvent } from 'react'

import {
  flowNodeCreateSourceHandleId,
  flowNodeCreateTargetHandleId,
  flowNodeEdgeSourceHandleId,
  flowNodeEdgeTargetHandleId,
} from '@/features/canvas/flownode/handles'

type ConnectionSide = 'left' | 'right'

interface MagneticOffset {
  x: number
  y: number
}

type MagneticOffsets = Record<ConnectionSide, MagneticOffset>

const magneticRadius = 32
const handleOutsideOffset = 12
const restingOffset: MagneticOffset = { x: 0, y: 0 }
const restingOffsets: MagneticOffsets = {
  left: restingOffset,
  right: restingOffset,
}

export const ConnectionHandles = memo(function ConnectionHandles({
  mode = 'node',
}: {
  mode?: 'node' | 'selection'
}) {
  const nodeId = useNodeId()
  const updateNodeInternals = useUpdateNodeInternals()
  const frameRef = useRef<number | null>(null)
  const [activeSide, setActiveSide] = useState<ConnectionSide | null>(null)
  const [magneticOffsets, setMagneticOffsets] = useState<MagneticOffsets>(restingOffsets)

  const followPointer = useCallback((event: PointerEvent<HTMLDivElement>, side: ConnectionSide) => {
    const nodeElement = event.currentTarget.closest('.react-flow__node')
    if (!(nodeElement instanceof HTMLElement)) return
    const bounds = nodeElement.getBoundingClientRect()
    if (bounds.width <= 0 || bounds.height <= 0 || nodeElement.offsetWidth <= 0) return

    const scale = bounds.width / nodeElement.offsetWidth
    const pointerX = (event.clientX - bounds.left) / scale
    const pointerY = (event.clientY - bounds.top) / scale
    const originX = side === 'left' ? -handleOutsideOffset : nodeElement.offsetWidth + handleOutsideOffset
    const originY = nodeElement.offsetHeight / 2
    const deltaX = pointerX - originX
    const deltaY = pointerY - originY
    const distance = Math.hypot(deltaX, deltaY)
    const ratio = distance > magneticRadius ? magneticRadius / distance : 1
    const nextOffset = {
      x: deltaX * ratio,
      y: deltaY * ratio,
    }

    if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
    frameRef.current = requestAnimationFrame(() => {
      setActiveSide(side)
      setMagneticOffsets((current) => ({ ...current, [side]: nextOffset }))
      frameRef.current = null
    })
  }, [])

  useEffect(() => {
    if (nodeId !== null) updateNodeInternals(nodeId)
  }, [magneticOffsets, nodeId, updateNodeInternals])

  useEffect(() => () => {
    if (frameRef.current !== null) cancelAnimationFrame(frameRef.current)
  }, [])

  const targetOffset = magneticOffsets.left
  const sourceOffset = magneticOffsets.right
  const targetHandleStyle = {
    width: 10,
    height: 10,
    border: 0,
    backgroundColor: 'var(--color-link)',
    left: `calc(-${handleOutsideOffset}px + ${targetOffset.x}px)`,
    top: `calc(50% + ${targetOffset.y}px)`,
  }
  const sourceHandleStyle = {
    width: 10,
    height: 10,
    border: 0,
    backgroundColor: 'var(--color-link)',
    right: `calc(-${handleOutsideOffset}px - ${sourceOffset.x}px)`,
    top: `calc(50% + ${sourceOffset.y}px)`,
  }

  return (
    <div
      className={`warmnote-flow__connection-controls warmnote-flow__connection-controls--${mode}`}
      onPointerLeave={() => setActiveSide(null)}
    >
      <div
        aria-hidden="true"
        className="warmnote-flow__connection-proximity warmnote-flow__connection-proximity--left nodrag nopan"
        onPointerMove={(event) => followPointer(event, 'left')}
      />
      <div
        aria-hidden="true"
        className="warmnote-flow__connection-proximity warmnote-flow__connection-proximity--right nodrag nopan"
        onPointerMove={(event) => followPointer(event, 'right')}
      />
      <Handle
        id={flowNodeEdgeTargetHandleId}
        className="warmnote-flow__edge-anchor warmnote-flow__edge-anchor--target"
        isConnectable={false}
        type="target"
        position={Position.Left}
      />
      <Handle
        id={flowNodeEdgeSourceHandleId}
        className="warmnote-flow__edge-anchor warmnote-flow__edge-anchor--source"
        isConnectable={false}
        type="source"
        position={Position.Right}
      />
      <Handle
        id={flowNodeCreateTargetHandleId}
        className="warmnote-flow__connection-trigger warmnote-flow__connection-trigger--target"
        data-active={activeSide === 'left' || undefined}
        style={targetHandleStyle}
        type="target"
        position={Position.Left}
      />
      <Handle
        id={flowNodeCreateSourceHandleId}
        className="warmnote-flow__connection-trigger warmnote-flow__connection-trigger--source"
        data-active={activeSide === 'right' || undefined}
        style={sourceHandleStyle}
        type="source"
        position={Position.Right}
      />
    </div>
  )
})
