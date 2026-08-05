import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import {
  flowNodeEdgeSourceHandleId,
  flowNodeEdgeTargetHandleId,
} from '@/features/canvas/flownode/handles'
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

export function toFlowEdges(edges: CanvasEdge[], candidates: AgentCandidate[]): CanvasFlowEdge[] {
  const result = new Array<CanvasFlowEdge>(
    edges.length + candidates.reduce((count, candidate) => count + candidate.contextNodeIds.length, 0),
  )
  let resultIndex = 0

  for (const edge of edges) {
    result[resultIndex] = {
      id: edge.id,
      type: 'canvas-edge',
      source: edge.sourceNodeId,
      sourceHandle: flowNodeEdgeSourceHandleId,
      target: edge.targetNodeId,
      targetHandle: flowNodeEdgeTargetHandleId,
      selectable: true,
      deletable: true,
      interactionWidth: 28,
      style: { stroke: 'var(--color-mute)', strokeWidth: 1.25 },
      data: { kind: edge.kind, persisted: true, workId: edge.workId },
    }
    resultIndex += 1
  }

  for (const candidate of candidates) {
    for (const sourceNodeId of candidate.contextNodeIds) {
      result[resultIndex] = {
        id: `candidate-context:${candidate.id}:${sourceNodeId}`,
        type: 'canvas-edge',
        source: sourceNodeId,
        sourceHandle: flowNodeEdgeSourceHandleId,
        target: `candidate:${candidate.id}`,
        targetHandle: flowNodeEdgeTargetHandleId,
        selectable: false,
        deletable: false,
        style: {
          stroke: 'var(--color-link)',
          strokeWidth: 1.5,
          strokeDasharray: '5 5',
        },
        data: { kind: 'candidate_context', persisted: false, workId: candidate.workId },
      }
      resultIndex += 1
    }
  }

  return result
}
