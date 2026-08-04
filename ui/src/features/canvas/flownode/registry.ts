import type { NodeTypes } from '@xyflow/react'

import { FlowNodeRenderer } from '@/features/canvas/flownode/components/FlowNodeRenderer'

export const flowNodeTypes = {
  'flow-node': FlowNodeRenderer,
} satisfies NodeTypes
