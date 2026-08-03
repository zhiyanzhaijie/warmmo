export type WorkStatus = 'draft' | 'initializing' | 'failed'

export type PreviewNodeKind = 'chapter' | 'character' | 'plot' | 'world'

export interface WorkPreviewNode {
  id: string
  label: string
  kind: PreviewNodeKind
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
