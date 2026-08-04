import type { Node } from '@xyflow/react'

import type { CanvasNodeKind } from '@/types/canvas'

export type FlowNodeSourceType = 'node' | 'candidate'

export type FlowNodeDetailLevel = 'full' | 'compact' | 'marker'

export interface FlowNodeData extends Record<string, unknown> {
  workId: string
  sourceId: string
  sourceType: FlowNodeSourceType
  kind: CanvasNodeKind
  title: string
  content: string
  revision: number
  layerId: string
  contextTags: string[]
}

export type StoryFlowNode = Node<FlowNodeData, 'flow-node'>
