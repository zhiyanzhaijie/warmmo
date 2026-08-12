import type { NodeProps } from '@xyflow/react'
import { memo } from 'react'

import { flowNodeRenderers } from '@/features/canvas/flownode/renderers'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'

export const FlowNodeRenderer = memo(function FlowNodeRenderer(props: NodeProps<StoryFlowNode>) {
  const detailLevel = useFlowNodeStore((state) => state.detailLevel)
  const renderers = flowNodeRenderers[props.data.kind]
  if (renderers === undefined) return null
  const Renderer = renderers[detailLevel]
  return <Renderer {...props} />
})
