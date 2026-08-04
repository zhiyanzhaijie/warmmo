import type { CanvasNodeKind } from '@/features/canvas/nodes/definitions'

export type WorkStatus = 'draft' | 'initializing' | 'failed'

export interface WorkPreviewNode {
  id: string
  label: string
  kind: CanvasNodeKind
  x: number
  y: number
}

export interface WorkPreviewEdge {
  source: string
  target: string
}

export interface WorkSummary {
  id: string
  title: string
  updatedLabel: string
  nodeCount: number
  modelName: string
  status: WorkStatus
  previewNodes: WorkPreviewNode[]
  previewEdges: WorkPreviewEdge[]
}
