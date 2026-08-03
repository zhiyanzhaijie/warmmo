export type CanvasNodeKind = 'chapter' | 'character' | 'plot' | 'world' | 'setting' | 'item' | 'note' | 'timeline'

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

export interface AgentCandidate {
  id: string
  runId: string
  workId: string
  skillId: string
  skillVersion: string
  content: string
  createdAt: string
}

export type AgentRunStatus = 'queued' | 'running' | 'waiting-for-user' | 'completed' | 'failed' | 'cancelled'

export interface AgentRun {
  id: string
  workId: string
  status: AgentRunStatus
  prompt: string
  target: string
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
