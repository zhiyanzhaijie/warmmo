import type { EdgeTypes, NodeTypes } from '@xyflow/react'

import { FlowNodeRenderer } from '@/features/canvas/flownode/components/FlowNodeRenderer'
import { SelectionProxyFlowNodeRenderer } from '@/features/canvas/flownode/components/SelectionProxyFlowNode'
import { CanvasFlowEdge } from '@/features/canvas/flowedge/CanvasFlowEdge'

export const flowNodeTypes = {
  'flow-node': FlowNodeRenderer,
  'selection-proxy': SelectionProxyFlowNodeRenderer,
} satisfies NodeTypes

export const flowEdgeTypes = {
  'canvas-edge': CanvasFlowEdge,
} satisfies EdgeTypes
