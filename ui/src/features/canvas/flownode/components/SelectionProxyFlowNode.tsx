import type { NodeProps } from '@xyflow/react'
import { memo } from 'react'

import { ConnectionHandles } from '@/features/canvas/flownode/components/ConnectionHandles'
import type { SelectionProxyFlowNode } from '@/features/canvas/flownode/types'

export const SelectionProxyFlowNodeRenderer = memo(function SelectionProxyFlowNodeRenderer({
  data,
}: NodeProps<SelectionProxyFlowNode>) {
  return (
    <div
      aria-label={`已选择 ${data.contextNodeIds.length} 个节点`}
      className="warmmo-flow__selection-proxy"
    >
      <ConnectionHandles mode="selection" />
    </div>
  )
})
