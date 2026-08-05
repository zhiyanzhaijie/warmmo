import type { CanvasNodeKind } from '@/features/canvas/nodes/definitions'

export type { CanvasNodeKind } from '@/features/canvas/nodes/definitions'

export interface CanvasNode {
  id: string
  workId: string
  revision: number
  kind: CanvasNodeKind
  title: string
  content: string
  x: number
  y: number
  createdAt: string
  updatedAt: string
}
export interface CanvasEdge {
  id: string
  workId: string
  sourceNodeId: string
  targetNodeId: string
  kind: 'generated_from'
  createdAt: string
}

export interface CanvasNodePosition {
  nodeId: string
  x: number
  y: number
}

export interface CanvasHistoryState {
  canUndo: boolean
  canRedo: boolean
  undoLabel: string
  redoLabel: string
}

export interface AgentCandidate {
  id: string
  runId: string
  workId: string
  skillId: string
  skillVersion: string
  status: 'pending' | 'accepted' | 'rejected'
  kind: CanvasNodeKind
  title: string
  content: string
  x: number
  y: number
  contextNodeIds: string[]
  acceptedNodeId?: string
  createdAt: string
  decidedAt?: string
}

export type AgentRunStatus = 'queued' | 'running' | 'waiting_input' | 'completed' | 'failed' | 'cancelled'

export interface AgentRun {
  id: string
  workId: string
  status: AgentRunStatus
  prompt: string
  target: string
  targetNodeId?: string
  providerId: string
  modelId: string
  contextNodeIds: string[]
  errorMessage?: string
  createdAt: string
  updatedAt: string
}

export interface AgentEvent {
  id: string
  runId: string
  sequence: number
  type: string
  timestamp: string
  data: Record<string, unknown> | null
}
