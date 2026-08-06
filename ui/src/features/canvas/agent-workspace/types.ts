import type { CanvasNodeKind } from '@/types/canvas'

export interface CanvasContextNode {
  id: string
  kind: CanvasNodeKind
  title: string
  content: string
}
