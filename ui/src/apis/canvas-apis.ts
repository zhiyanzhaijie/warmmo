import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { useContextAgentAvailability } from '@/apis/model-apis'
import { coreClient } from '@/lib/api/core-client'
import type {
  AgentCandidate,
  AgentRun,
  CanvasEdge,
  CanvasHistoryState,
  CanvasNode,
  CanvasNodeKind,
  CanvasNodePosition,
  CanvasNodeVersion,
} from '@/types/canvas'
import type { ModelReference } from '@/types/provider'

interface NodesResponse {
  nodes: CanvasNode[]
}

interface CandidatesResponse {
  candidates: AgentCandidate[]
}

interface EdgesResponse {
  edges: CanvasEdge[]
}

export const canvasKeys = {
  nodes: (workId: string) => ['canvas', workId, 'nodes'] as const,
  node: (workId: string, nodeId: string) => ['canvas', workId, 'nodes', nodeId] as const,
  nodeVersions: (workId: string, nodeId: string) => ['canvas', workId, 'nodes', nodeId, 'versions'] as const,
  edges: (workId: string) => ['canvas', workId, 'edges'] as const,
  candidates: (workId: string) => ['canvas', workId, 'candidates'] as const,
  history: (workId: string) => ['canvas', workId, 'history'] as const,
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

export function useUpdateCanvasNode(workId: string, nodeId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { title: string; content: string; expectedRevision: number }) =>
      coreClient<CanvasNode>(`/works/${workId}/nodes/${nodeId}`, {
        method: 'PATCH',
        body: input,
      }),
    onSuccess: (node) => {
      queryClient.setQueryData(canvasKeys.node(workId, nodeId), node)
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) =>
        nodes.map((existing) => existing.id === node.id ? node : existing))
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
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
    mutationFn: (input: {
      kind: CanvasNodeKind
      title: string
      content: string
      x: number
      y: number
      contextNodeIds?: string[]
    }) =>
      coreClient<CanvasNode>(`/works/${workId}/nodes`, { method: 'POST', body: input }),
    onSuccess: (node) => {
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) => [...nodes, node])
      void queryClient.invalidateQueries({ queryKey: canvasKeys.edges(workId) })
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
    },
  })
}

export function useCanvasEdges(workId: string) {
  return useQuery({
    queryKey: canvasKeys.edges(workId),
    queryFn: async ({ signal }) => {
      const response = await coreClient<EdgesResponse>(`/works/${workId}/edges`, { signal })
      return response.edges
    },
  })
}

export function useCreateCanvasEdge(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { sourceNodeId: string; targetNodeId: string }) =>
      coreClient<CanvasEdge>(`/works/${workId}/edges`, { method: 'POST', body: input }),
    onSuccess: (edge) => {
      queryClient.setQueryData<CanvasEdge[]>(canvasKeys.edges(workId), (edges = []) =>
        edges.some((existing) => existing.id === edge.id) ? edges : [...edges, edge])
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
    },
  })
}

export function useDeleteCanvasEdges(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (edgeIds: string[]) =>
      coreClient<void>(`/works/${workId}/edges`, { method: 'DELETE', body: { edgeIds } }),
    onSuccess: (_, edgeIds) => {
      const deletedEdgeIds = new Set(edgeIds)
      queryClient.setQueryData<CanvasEdge[]>(canvasKeys.edges(workId), (edges = []) =>
        edges.filter((edge) => !deletedEdgeIds.has(edge.id)))
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
    },
  })
}

export function useCanvasNode(workId: string, nodeId: string | null) {
  const queryClient = useQueryClient()
  return useQuery({
    queryKey: canvasKeys.node(workId, nodeId ?? ''),
    enabled: nodeId !== null,
    initialData: () => nodeId === null
      ? undefined
      : queryClient.getQueryData<CanvasNode[]>(canvasKeys.nodes(workId))
          ?.find((node) => node.id === nodeId),
    queryFn: ({ signal }) => coreClient<CanvasNode>(`/works/${workId}/nodes/${nodeId}`, { signal }),
  })
}

interface NodeVersionsResponse {
  versions: CanvasNodeVersion[]
}

export function useCanvasNodeVersions(workId: string, nodeId: string | null) {
  return useQuery({
    queryKey: canvasKeys.nodeVersions(workId, nodeId ?? ''),
    enabled: nodeId !== null,
    queryFn: ({ signal }) => coreClient<NodeVersionsResponse>(
      `/works/${workId}/nodes/${nodeId}/versions`,
      { signal },
    ).then((response) => response.versions),
  })
}

export function useSwitchCanvasNodeVersion(workId: string, nodeId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (versionId: string) => coreClient<CanvasNode>(
      `/works/${workId}/nodes/${nodeId}/versions/current`,
      { method: 'POST', body: { versionId } },
    ),
    onSuccess: (node) => {
      queryClient.setQueryData(canvasKeys.node(workId, nodeId), node)
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) =>
        nodes.map((existing) => existing.id === node.id ? node : existing))
      void queryClient.invalidateQueries({ queryKey: canvasKeys.nodeVersions(workId, nodeId) })
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
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

export function useUpdateCanvasNodePositions(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (positions: CanvasNodePosition[]) =>
      coreClient<void>(`/works/${workId}/nodes/positions`, {
        method: 'PATCH',
        body: { positions },
      }),
    onSuccess: (_, positions) => {
      const positionByNodeId = new Map(positions.map((position) => [position.nodeId, position]))
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) =>
        nodes.map((node) => {
          const position = positionByNodeId.get(node.id)
          return position === undefined ? node : { ...node, x: position.x, y: position.y }
        }))
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
    },
  })
}

export function useLayoutCanvasChapter(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (chapterOutlineNodeId: string) => coreClient<{ positions: CanvasNodePosition[] }>(
      `/works/${encodeURIComponent(workId)}/nodes/${encodeURIComponent(chapterOutlineNodeId)}/layout`,
      { method: 'POST' },
    ),
    onSuccess: ({ positions }) => {
      const positionByNodeId = new Map(positions.map((position) => [position.nodeId, position]))
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) =>
        nodes.map((node) => {
          const position = positionByNodeId.get(node.id)
          return position === undefined ? node : { ...node, x: position.x, y: position.y }
        }))
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
    },
  })
}

export function useDeleteCanvasNodes(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (nodeIds: string[]) =>
      coreClient<void>(`/works/${workId}/nodes`, {
        method: 'DELETE',
        body: { nodeIds },
      }),
    onSuccess: (_, nodeIds) => {
      const deletedNodeIds = new Set(nodeIds)
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) =>
        nodes.filter((node) => !deletedNodeIds.has(node.id)))
      queryClient.setQueryData<CanvasEdge[]>(canvasKeys.edges(workId), (edges = []) =>
        edges.filter((edge) => !deletedNodeIds.has(edge.sourceNodeId) && !deletedNodeIds.has(edge.targetNodeId)))
      void queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) })
    },
  })
}

export function useCanvasHistory(workId: string) {
  return useQuery({
    queryKey: canvasKeys.history(workId),
    queryFn: ({ signal }) => coreClient<CanvasHistoryState>(`/works/${workId}/canvas-history`, { signal }),
  })
}

export function useUndoCanvasAction(workId: string) {
  return useCanvasHistoryMutation(workId, 'undo')
}

export function useRedoCanvasAction(workId: string) {
  return useCanvasHistoryMutation(workId, 'redo')
}

function useCanvasHistoryMutation(workId: string, direction: 'undo' | 'redo') {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => coreClient<CanvasHistoryState>(`/works/${workId}/canvas-history/${direction}`, {
      method: 'POST',
    }),
    onSuccess: (history) => {
      queryClient.setQueryData(canvasKeys.history(workId), history)
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: canvasKeys.nodes(workId) }),
        queryClient.invalidateQueries({ queryKey: canvasKeys.edges(workId) }),
      ])
    },
  })
}

export function useUpdateCanvasCandidatePosition(workId: string) {
  return useMutation({
    mutationFn: (input: { candidateId: string; x: number; y: number }) =>
      coreClient<void>(`/works/${workId}/candidates/${input.candidateId}/position`, {
        method: 'PATCH',
        body: { x: input.x, y: input.y },
      }),
  })
}

export function useAcceptCanvasCandidate(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { candidateId: string; title: string }) =>
      coreClient<CanvasNode>(`/works/${workId}/candidates/${input.candidateId}/accept`, {
        method: 'POST',
        body: { title: input.title },
      }),
    onSuccess: (node, input) => {
      queryClient.setQueryData<CanvasNode[]>(canvasKeys.nodes(workId), (nodes = []) => {
        if (nodes.some((existing) => existing.id === node.id)) {
          return nodes.map((existing) => existing.id === node.id ? node : existing)
        }
        return [...nodes, node]
      })
      queryClient.setQueryData(canvasKeys.node(workId, node.id), node)
      queryClient.setQueryData<AgentCandidate[]>(canvasKeys.candidates(workId), (candidates = []) =>
        candidates.filter((candidate) => candidate.id !== input.candidateId))
      void queryClient.invalidateQueries({ queryKey: canvasKeys.edges(workId) })
    },
  })
}

export function useRejectCanvasCandidate(workId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (candidateId: string) =>
      coreClient<void>(`/works/${workId}/candidates/${candidateId}/reject`, { method: 'POST' }),
    onSuccess: (_, candidateId) => {
      queryClient.setQueryData<AgentCandidate[]>(canvasKeys.candidates(workId), (candidates = []) =>
        candidates.filter((candidate) => candidate.id !== candidateId))
    },
  })
}

export function useCreateAgentRun(workId: string) {
  return useMutation({
    mutationFn: (input: { prompt: string; targetNodeId: string; contextNodeIds: string[]; model: ModelReference }) =>
      coreClient<AgentRun>(`/works/${workId}/agent-runs`, {
        method: 'POST',
        body: {
          prompt: input.prompt,
          contextNodeIds: input.contextNodeIds,
          target: 'node-update',
          targetNodeId: input.targetNodeId,
          providerId: input.model.providerId,
          modelId: input.model.modelId,
        },
      }),
  })
}

export type CollaborativeAgentTarget = 'collaborative-targeted' | 'collaborative-explore'

export function useCreateCollaborativeAgentRun(workId: string) {
  const contextAgent = useContextAgentAvailability()
  const mutation = useMutation({
    mutationFn: (input: {
      prompt: string
      target: CollaborativeAgentTarget
      contextNodeIds: string[]
      model: ModelReference
    }) => {
      if (!contextAgent.isAvailable) {
        return Promise.reject(new Error('配置 embedding 模型后才能使用画布上下文 Agent'))
      }
      return coreClient<AgentRun>(`/works/${workId}/agent-runs`, {
        method: 'POST',
        body: {
          prompt: input.prompt,
          contextNodeIds: input.contextNodeIds,
          target: input.target,
          providerId: input.model.providerId,
          modelId: input.model.modelId,
        },
      })
    },
  })
  return { ...mutation, contextAgentAvailable: contextAgent.isAvailable, contextAgentPending: contextAgent.isPending }
}

export type NodeDerivationTarget = 'section-outline-batch' | 'chapter-section' | 'chapter-archive'

export function useCreateNodeDerivationRun(workId: string) {
  return useMutation({
    mutationFn: (input: {
      prompt: string
      target: NodeDerivationTarget
      targetNodeId: string
      contextNodeIds: string[]
      model: ModelReference
    }) => coreClient<AgentRun>(`/works/${workId}/agent-runs`, {
      method: 'POST',
      body: {
        prompt: input.prompt,
        contextNodeIds: input.contextNodeIds,
        target: input.target,
        targetNodeId: input.targetNodeId,
        providerId: input.model.providerId,
        modelId: input.model.modelId,
      },
    }),
  })
}

export function useRespondToAgentRun() {
  return useMutation({
    mutationFn: (input: { runId: string; approvalEventId: string; answer: string }) =>
      coreClient<AgentRun>(`/agent-runs/${input.runId}/responses`, {
        method: 'POST',
        body: { approvalEventId: input.approvalEventId, answer: input.answer },
      }),
  })
}
