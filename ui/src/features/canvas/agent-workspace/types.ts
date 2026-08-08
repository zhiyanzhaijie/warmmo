import type { CanvasNodeKind } from '@/types/canvas'

export interface CanvasContextNode {
  id: string
  kind: CanvasNodeKind
  title: string
  content: string
}

export interface CanvasAgentPromptSubmission {
  contextNodeIds: string[]
  prompt: string
}

export interface PendingAgentInput {
  runId: string
  approvalEventId: string
  question: string
  options: string[]
  lastSequence: number
}
