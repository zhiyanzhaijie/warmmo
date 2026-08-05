import type { Edge } from '@xyflow/react'

export interface CanvasFlowEdgeData extends Record<string, unknown> {
  kind: string
  persisted: boolean
  workId: string
  onDelete?: (edgeId: string) => void
}

export type CanvasFlowEdge = Edge<CanvasFlowEdgeData, 'canvas-edge'>
