import { Handle, Position } from '@xyflow/react'
import { memo } from 'react'

export const FlowNodeControls = memo(function FlowNodeControls() {
  return (
    <>
      <Handle
        className="!size-2 !border-canvas-elevated !bg-mute opacity-0 group-hover:opacity-100"
        type="target"
        position={Position.Left}
      />
      <Handle
        className="!size-2 !border-canvas-elevated !bg-mute opacity-0 group-hover:opacity-100"
        type="source"
        position={Position.Right}
      />
    </>
  )
})
