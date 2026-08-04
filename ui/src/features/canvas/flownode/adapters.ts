import type { Edge } from '@xyflow/react'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'
import type { AgentCandidate, CanvasEdge, CanvasNode } from '@/types/canvas'

export function toFlowNodes(nodes: CanvasNode[], candidates: AgentCandidate[]): StoryFlowNode[] {
  const result = new Array<StoryFlowNode>(nodes.length + candidates.length)

  for (let index = 0; index < nodes.length; index += 1) {
    const node = nodes[index]
    result[index] = {
      id: node.id,
      type: 'flow-node',
      position: { x: node.x, y: node.y },
      data: {
        workId: node.workId,
        sourceId: node.id,
        sourceType: 'node',
        kind: node.kind,
        title: node.title,
        content: node.content,
        revision: node.revision,
        layerId: 'main',
        contextTags: [],
      },
    }
  }

  for (let index = 0; index < candidates.length; index += 1) {
    const candidate = candidates[index]
    result[nodes.length + index] = {
      id: `candidate:${candidate.id}`,
      type: 'flow-node',
      position: { x: candidate.x, y: candidate.y },
      data: {
        workId: candidate.workId,
        sourceId: candidate.id,
        sourceType: 'candidate',
        kind: candidate.kind,
        title: candidate.title,
        content: candidate.content,
        revision: 0,
        layerId: 'candidate',
        contextTags: [],
      },
    }
  }

  return result
}

export function toFlowEdges(edges: CanvasEdge[], candidates: AgentCandidate[]): Edge[] {
  const result = new Array<Edge>(
    edges.length + candidates.reduce((count, candidate) => count + candidate.contextNodeIds.length, 0),
  )
  let resultIndex = 0

  for (const edge of edges) {
    result[resultIndex] = {
      id: edge.id,
      source: edge.sourceNodeId,
      target: edge.targetNodeId,
      selectable: false,
      deletable: false,
      style: { stroke: 'var(--color-mute)', strokeWidth: 1.25 },
      data: { kind: edge.kind },
    }
    resultIndex += 1
  }

  for (const candidate of candidates) {
    for (const sourceNodeId of candidate.contextNodeIds) {
      result[resultIndex] = {
        id: `candidate-context:${candidate.id}:${sourceNodeId}`,
        source: sourceNodeId,
        target: `candidate:${candidate.id}`,
        selectable: false,
        deletable: false,
        style: {
          stroke: 'var(--color-link)',
          strokeWidth: 1.5,
          strokeDasharray: '5 5',
        },
        data: { kind: 'candidate_context' },
      }
      resultIndex += 1
    }
  }

  return result
}
