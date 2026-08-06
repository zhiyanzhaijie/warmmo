import { useEffect, useMemo } from 'react'

import {
  type FlowNodeArchiveOptions,
  toFlowEdges,
  toFlowNodes,
} from '@/features/canvas/flownode/adapters'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { AgentCandidate, CanvasEdge, CanvasNode } from '@/types/canvas'

export function useSyncFlowNodes(
  nodes: CanvasNode[] | undefined,
  candidates: AgentCandidate[] | undefined,
  edges: CanvasEdge[] | undefined,
  hiddenNodeIds: ReadonlySet<string>,
  collapsedNodeProxyIds: ReadonlyMap<string, string>,
  archiveOptions: FlowNodeArchiveOptions,
) {
  const syncNodes = useFlowNodeStore((state) => state.actions.syncNodes)
  const syncEdges = useFlowNodeStore((state) => state.actions.syncEdges)
  const flowNodes = useMemo(
    () => toFlowNodes(nodes ?? [], candidates ?? [], hiddenNodeIds, archiveOptions),
    [archiveOptions, candidates, hiddenNodeIds, nodes],
  )
  const flowEdges = useMemo(
    () => toFlowEdges(edges ?? [], candidates ?? [], collapsedNodeProxyIds),
    [candidates, collapsedNodeProxyIds, edges],
  )

  useEffect(() => {
    syncNodes(flowNodes)
  }, [flowNodes, syncNodes])

  useEffect(() => {
    syncEdges(flowEdges)
  }, [flowEdges, syncEdges])
}
