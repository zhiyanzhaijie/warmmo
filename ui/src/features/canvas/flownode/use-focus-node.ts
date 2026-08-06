import { useReactFlow } from '@xyflow/react'
import { useCallback } from 'react'

import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { CanvasFlowNode } from '@/features/canvas/flownode/types'

export function useFocusNode() {
  const flow = useReactFlow<CanvasFlowNode>()
  const focusSourceNode = useFlowNodeStore((state) => state.actions.focusSourceNode)

  return useCallback((nodeId: string) => {
    focusSourceNode(nodeId)
    const node = flow.getNode(nodeId)
    if (node === undefined) return

    void flow.fitView({
      nodes: [node],
      duration: 220,
      padding: 0.75,
      maxZoom: 1.15,
    })
  }, [flow, focusSourceNode])
}
