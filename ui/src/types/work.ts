import type { CanvasNodeKind } from '@/features/canvas/nodes/definitions'

export type WorkStatus = 'active' | 'archived'

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
  description: string
  folderId: string
  folderName: string
  status: WorkStatus
  revision: number
  updatedAt: string
  nodeCount: number
  previewNodes: WorkPreviewNode[]
  previewEdges: WorkPreviewEdge[]
}

export interface WorkDetail {
  id: string
  title: string
  description: string
  folderId: string
  folderName: string
  status: WorkStatus
  revision: number
  updatedAt: string
}

export interface WorkFolder {
  id: string
  name: string
  sortOrder: number
  createdAt: string
  updatedAt: string
}

export interface CreateWorkInput {
  title: string
  description: string
  folderId: string
}

export interface UpdateWorkInput extends CreateWorkInput {
  id: string
  status: WorkStatus
  expectedRevision: number
}
