import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { coreClient } from '@/lib/api/core-client'
import type { AgentCandidate, AgentRun, CanvasNode, CanvasNodeKind } from '@/types/canvas'
import type { ModelReference } from '@/types/provider'

interface NodesResponse {
  nodes: CanvasNode[]
}

interface CandidatesResponse {
  candidates: AgentCandidate[]
}

export const canvasKeys = {
  nodes: (workId: string) => ['canvas', workId, 'nodes'] as const,
  candidates: (workId: string) => ['canvas', workId, 'candidates'] as const,
}

export function useCanvasNodes(workId: string) {
  return useQuery({
    queryKey: canvasKeys.nodes(workId),
    queryFn: async ({ signal }) => {
      const response = await coreClient<NodesResponse>(`/works/${workId}/nodes`, { signal })
      return response.nodes
    },
  })
}

export function useCanvasCandidates(workId: string) {
  return useQuery({
    queryKey: canvasKeys.candidates(workId),
    queryFn: async ({ signal }) => {
      const response = await coreClient<CandidatesResponse>(`/works/${workId}/candidates`, { signal })
      return response.candidates
    },
  })
}

export function useCreateCanvasNode(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { kind: CanvasNodeKind; title: string; content: string; x: number; y: number }) =>
      coreClient<CanvasNode>(`/works/${workId}/nodes`, { method: 'POST', body: input }),
    onSuccess: (node) => {
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) => [...nodes, node])
    },
  })
}

export function useUpdateCanvasNodePosition(workId: string) {
  return useMutation({
    mutationFn: (input: { nodeId: string; x: number; y: number }) =>
      coreClient<void>(`/works/${workId}/nodes/${input.nodeId}/position`, {
        method: 'PATCH',
        body: { x: input.x, y: input.y },
      }),
  })
}

export function useCreateAgentRun(workId: string) {
  return useMutation({
    mutationFn: (input: { prompt: string; contextNodeIds: string[]; model: ModelReference }) =>
      coreClient<AgentRun>(`/works/${workId}/agent-runs`, {
        method: 'POST',
        body: {
          prompt: input.prompt,
          contextNodeIds: input.contextNodeIds,
          target: 'chapter',
          providerId: input.model.providerId,
          modelId: input.model.modelId,
        },
      }),
  })
}
