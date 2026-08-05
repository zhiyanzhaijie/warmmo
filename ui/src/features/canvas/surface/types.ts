import type { Position, XYPosition } from '@xyflow/react'

import type { CanvasNodeKind } from '@/features/canvas/nodes/definitions'

export interface NodeCreationSession {
  id: number
  contextNodeIds: string[]
  dropPoint: XYPosition
  sourcePoint: XYPosition | null
  sourcePosition: Position | null
}

export interface CanvasNodeCommandMenuProps {
  disabled?: boolean
  label: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (kind: CanvasNodeKind) => void
}
