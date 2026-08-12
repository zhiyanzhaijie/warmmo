import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import {
  flowNodeEdgeSourceHandleId,
  flowNodeEdgeTargetHandleId,
} from '@/features/canvas/flownode/handles'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'
import {
  isCanvasNodeKind,
  isChapterArchiveProtectedNodeKind,
} from '@/features/canvas/nodes/definitions'
import type { AgentCandidate, CanvasEdge, CanvasNode } from '@/types/canvas'

const emptyHiddenNodeIds: ReadonlySet<string> = new Set()
const emptyNodeProxyIds: ReadonlyMap<string, string> = new Map()

export interface FlowNodeArchiveOptions {
  stateResolved: boolean
  lockedNodeIds: ReadonlySet<string>
  expandedChapterNodeIds: ReadonlySet<string>
  layoutPending: boolean
  layoutPendingChapterNodeId: string | null
  onToggleChapter: (chapterNodeId: string) => void
  onLayoutChapter: (chapterNodeId: string) => void
}

const defaultArchiveOptions: FlowNodeArchiveOptions = {
  stateResolved: false,
  lockedNodeIds: emptyHiddenNodeIds,
  expandedChapterNodeIds: emptyHiddenNodeIds,
  layoutPending: false,
  layoutPendingChapterNodeId: null,
  onToggleChapter: () => undefined,
  onLayoutChapter: () => undefined,
}

export function toFlowNodes(
  nodes: CanvasNode[],
  candidates: AgentCandidate[],
  hiddenNodeIds: ReadonlySet<string> = emptyHiddenNodeIds,
  archiveOptions: FlowNodeArchiveOptions = defaultArchiveOptions,
): StoryFlowNode[] {
  const result: StoryFlowNode[] = []

  for (const node of nodes) {
    if (!isCanvasNodeKind(node.kind)) continue
    if (hiddenNodeIds.has(node.id)) continue
    const archiveProtected = isChapterArchiveProtectedNodeKind(node.kind)
    const archiveLocked = archiveProtected && archiveOptions.lockedNodeIds.has(node.id)
    const isArchivedChapter = archiveLocked && node.kind === 'chapter-outline'
    result.push({
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
        archiveStateResolved: !archiveProtected || archiveOptions.stateResolved,
        archiveLocked,
        archiveExpanded: isArchivedChapter
          ? archiveOptions.expandedChapterNodeIds.has(node.id)
          : undefined,
        archiveLayoutPending: isArchivedChapter
          ? archiveOptions.layoutPendingChapterNodeId === node.id
          : undefined,
        archiveLayoutDisabled: isArchivedChapter ? archiveOptions.layoutPending : undefined,
        onToggleArchive: isArchivedChapter ? archiveOptions.onToggleChapter : undefined,
        onLayoutArchive: isArchivedChapter ? archiveOptions.onLayoutChapter : undefined,
      },
    })
  }

  for (const candidate of candidates) {
    if (!isCanvasNodeKind(candidate.kind)) continue
    if (candidate.nodeId !== undefined && hiddenNodeIds.has(candidate.nodeId)) continue
    result.push({
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
        archiveStateResolved: true,
        archiveLocked: false,
        candidateType: candidate.candidateType,
        candidateReason: candidate.reason,
        candidateScore: candidate.changeScore,
      },
    })
  }

  return result
}

export function toFlowEdges(
  edges: CanvasEdge[],
  candidates: AgentCandidate[],
  collapsedNodeProxyIds: ReadonlyMap<string, string> = emptyNodeProxyIds,
): CanvasFlowEdge[] {
  const result: CanvasFlowEdge[] = []
  const edgeKeys = new Set<string>()

  for (const edge of edges) {
    const sourceNodeId = collapsedNodeProxyIds.get(edge.sourceNodeId) ?? edge.sourceNodeId
    const targetNodeId = collapsedNodeProxyIds.get(edge.targetNodeId) ?? edge.targetNodeId
    if (sourceNodeId === targetNodeId) continue
    const edgeKey = `${sourceNodeId}\u0000${targetNodeId}\u0000${edge.kind}`
    if (edgeKeys.has(edgeKey)) continue
    edgeKeys.add(edgeKey)
    const collapsed = sourceNodeId !== edge.sourceNodeId || targetNodeId !== edge.targetNodeId
    result.push({
      id: collapsed ? `collapsed:${sourceNodeId}:${targetNodeId}:${edge.kind}` : edge.id,
      type: 'canvas-edge',
      source: sourceNodeId,
      sourceHandle: flowNodeEdgeSourceHandleId,
      target: targetNodeId,
      targetHandle: flowNodeEdgeTargetHandleId,
      selectable: !collapsed,
      deletable: !collapsed,
      interactionWidth: 28,
      style: { stroke: 'var(--color-mute)', strokeWidth: 1.25 },
      data: { kind: edge.kind, persisted: !collapsed, workId: edge.workId },
    })
  }

  for (const candidate of candidates) {
    if (candidate.nodeId !== undefined && collapsedNodeProxyIds.has(candidate.nodeId)) continue
    for (const sourceNodeId of candidate.contextNodeIds ?? []) {
      const visibleSourceNodeId = collapsedNodeProxyIds.get(sourceNodeId) ?? sourceNodeId
      const edgeKey = `${visibleSourceNodeId}\u0000candidate:${candidate.id}\u0000candidate_context`
      if (edgeKeys.has(edgeKey)) continue
      edgeKeys.add(edgeKey)
      result.push({
        id: `candidate-context:${candidate.id}:${visibleSourceNodeId}`,
        type: 'canvas-edge',
        source: visibleSourceNodeId,
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
      })
    }
  }

  return result
}
