import type { NodeProps } from '@xyflow/react'
import type { ComponentType } from 'react'

import {
  CompactFlowNodeRenderer,
  MarkerFlowNodeRenderer,
} from '@/features/canvas/flownode/components/DegradedFlowNodes'
import { StoryFlowNodeRenderer } from '@/features/canvas/flownode/components/StoryFlowNode'
import type { FlowNodeDetailLevel, StoryFlowNode } from '@/features/canvas/flownode/types'
import { canvasNodeKinds, type CanvasNodeKind } from '@/features/canvas/nodes/definitions'

type FlowNodeRenderer = ComponentType<NodeProps<StoryFlowNode>>
type FlowNodeRenderers = Record<FlowNodeDetailLevel, FlowNodeRenderer>

const standardRenderers: FlowNodeRenderers = {
  full: StoryFlowNodeRenderer,
  compact: CompactFlowNodeRenderer,
  marker: MarkerFlowNodeRenderer,
}

export const flowNodeRenderers = Object.fromEntries(
  canvasNodeKinds.map((kind) => [kind, standardRenderers]),
) as Record<CanvasNodeKind, FlowNodeRenderers>
