import { applyEdgeChanges, applyNodeChanges, type Edge, type OnEdgesChange, type OnNodesChange } from '@xyflow/react'
import { createContext, useContext } from 'react'
import { useStore } from 'zustand'
import { createStore, type StoreApi } from 'zustand/vanilla'

import type { FlowNodeData, FlowNodeDetailLevel, StoryFlowNode } from '@/features/canvas/flownode/types'
import type { AgentEvent, CanvasNodePosition } from '@/types/canvas'

export type NodeAgentRunStatus = 'submitting' | 'running' | 'waiting_input' | 'completed' | 'failed'
export type NodeAgentOperation = 'update' | 'derive'

export interface NodeAgentRunState {
  runId: string | null
  nodeId: string
  operation: NodeAgentOperation
  status: NodeAgentRunStatus
  events: AgentEvent[]
  error: string
}

interface FlowNodeStoreActions {
  syncNodes: (nodes: StoryFlowNode[]) => void
  syncEdges: (edges: Edge[]) => void
  onNodesChange: OnNodesChange<StoryFlowNode>
  onEdgesChange: OnEdgesChange<Edge>
  setNodePositions: (positions: CanvasNodePosition[]) => void
  setDetailLevel: (detailLevel: FlowNodeDetailLevel) => void
  showNodeToolbar: (nodeId: string) => void
  hideNodeToolbar: () => void
  openPreview: (nodeId: string) => void
  closePreview: () => void
  focusSourceNode: (nodeId: string) => void
  beginNodeAgentRun: (nodeId: string, operation?: NodeAgentOperation) => void
  attachNodeAgentRun: (nodeId: string, runId: string) => void
  appendNodeAgentEvent: (nodeId: string, event: AgentEvent) => void
  failNodeAgentRun: (nodeId: string, message: string) => void
}

export interface FlowNodeStore {
  nodes: StoryFlowNode[]
  edges: Edge[]
  sourceNodeCount: number
  selectedSourceNodeIds: string[]
  toolbarSourceNodeId: string | null
  previewNodeId: string | null
  detailLevel: FlowNodeDetailLevel
  pendingFocusSourceNodeId: string | null
  nodeAgentRuns: Record<string, NodeAgentRunState>
  actions: FlowNodeStoreActions
}

export type FlowNodeStoreApi = StoreApi<FlowNodeStore>

export function createFlowNodeStore(): FlowNodeStoreApi {
  return createStore<FlowNodeStore>()((set) => {
    const actions: FlowNodeStoreActions = {
      syncNodes: (incomingNodes) => {
        set((state) => {
          let nodes = mergeRemoteNodes(state.nodes, incomingNodes)
          const pendingFocusSourceNodeId = state.pendingFocusSourceNodeId
          const focusedNodeId = pendingFocusSourceNodeId !== null && nodes.some(
            (node) => node.data.sourceType === 'node' && node.data.sourceId === pendingFocusSourceNodeId,
          ) ? pendingFocusSourceNodeId : null
          if (focusedNodeId !== null) nodes = selectOnlySourceNode(nodes, focusedNodeId)
          const sourceNodeCount = countSourceNodes(nodes)
          const selectedSourceNodeIds = collectSelectedSourceNodeIds(nodes)
          return {
            nodes,
            sourceNodeCount,
            selectedSourceNodeIds: areStringArraysEqual(selectedSourceNodeIds, state.selectedSourceNodeIds)
              ? state.selectedSourceNodeIds
              : selectedSourceNodeIds,
            toolbarSourceNodeId: focusedNodeId !== null
              ? focusedNodeId
              : resolveToolbarSourceNodeId(state.toolbarSourceNodeId, selectedSourceNodeIds),
            pendingFocusSourceNodeId: focusedNodeId !== null ? null : pendingFocusSourceNodeId,
          }
        })
      },
      syncEdges: (incomingEdges) => {
        set((state) => ({
          edges: areRemoteEdgesEqual(state.edges, incomingEdges) ? state.edges : incomingEdges,
        }))
      },
      onNodesChange: (changes) => {
        set((state) => {
          const nodes = applyNodeChanges(changes, state.nodes)
          if (!changes.some(affectsSelection)) return { nodes }

          const selectedSourceNodeIds = collectSelectedSourceNodeIds(nodes)
          return {
            nodes,
            selectedSourceNodeIds: areStringArraysEqual(selectedSourceNodeIds, state.selectedSourceNodeIds)
              ? state.selectedSourceNodeIds
              : selectedSourceNodeIds,
            toolbarSourceNodeId: resolveToolbarSourceNodeId(state.toolbarSourceNodeId, selectedSourceNodeIds),
          }
        })
      },
      onEdgesChange: (changes) => {
        set((state) => ({ edges: applyEdgeChanges(changes, state.edges) }))
      },
      setNodePositions: (positions) => {
        const positionByNodeId = new Map(positions.map((position) => [position.nodeId, position]))
        set((state) => ({
          nodes: state.nodes.map((node) => {
            const position = positionByNodeId.get(node.id)
            return position === undefined || (node.position.x === position.x && node.position.y === position.y)
              ? node
              : { ...node, position: { x: position.x, y: position.y } }
          }),
        }))
      },
      setDetailLevel: (detailLevel) => {
        set((state) => state.detailLevel === detailLevel ? state : { detailLevel })
      },
      showNodeToolbar: (nodeId) => {
        set((state) => state.toolbarSourceNodeId === nodeId ? state : { toolbarSourceNodeId: nodeId })
      },
      hideNodeToolbar: () => {
        set((state) => state.toolbarSourceNodeId === null ? state : { toolbarSourceNodeId: null })
      },
      openPreview: (nodeId) => {
        set({ previewNodeId: nodeId })
      },
      closePreview: () => {
        set({ previewNodeId: null })
      },
      focusSourceNode: (nodeId) => {
        set((state) => {
          const nodeExists = state.nodes.some(
            (node) => node.data.sourceType === 'node' && node.data.sourceId === nodeId,
          )
          if (!nodeExists) return { pendingFocusSourceNodeId: nodeId }
          return {
            nodes: selectOnlySourceNode(state.nodes, nodeId),
            selectedSourceNodeIds: [nodeId],
            toolbarSourceNodeId: nodeId,
            pendingFocusSourceNodeId: null,
          }
        })
      },
      beginNodeAgentRun: (nodeId, operation = 'update') => {
        set((state) => ({
          nodeAgentRuns: {
            ...state.nodeAgentRuns,
            [nodeId]: { runId: null, nodeId, operation, status: 'submitting', events: [], error: '' },
          },
        }))
      },
      attachNodeAgentRun: (nodeId, runId) => {
        set((state) => {
          const current = state.nodeAgentRuns[nodeId]
          return {
            nodeAgentRuns: {
              ...state.nodeAgentRuns,
              [nodeId]: current === undefined
                ? { runId, nodeId, operation: 'update', status: 'running', events: [], error: '' }
                : { ...current, runId, status: 'running', error: '' },
            },
          }
        })
      },
      appendNodeAgentEvent: (nodeId, event) => {
        set((state) => {
          const current = state.nodeAgentRuns[nodeId]
          if (current === undefined) return state
          if (current.events.some((existing) => existing.id === event.id || existing.sequence === event.sequence)) return state
          if (event.type === 'run.completed') {
            const nodeAgentRuns = { ...state.nodeAgentRuns }
            delete nodeAgentRuns[nodeId]
            return { nodeAgentRuns }
          }
          const status = event.type === 'run.failed' || event.type === 'run.cancelled'
            ? 'failed'
            : event.type === 'approval.required'
              ? 'waiting_input'
              : 'running'
          return {
            nodeAgentRuns: {
              ...state.nodeAgentRuns,
              [nodeId]: {
                ...current,
                status,
                events: [...current.events, event],
                error: status === 'failed' ? agentEventMessage(event) : current.error,
              },
            },
          }
        })
      },
      failNodeAgentRun: (nodeId, message) => {
        set((state) => {
          const current = state.nodeAgentRuns[nodeId]
          return {
            nodeAgentRuns: {
              ...state.nodeAgentRuns,
              [nodeId]: current === undefined
                ? { runId: null, nodeId, operation: 'update', status: 'failed', events: [], error: message }
                : { ...current, status: 'failed', error: message },
            },
          }
        })
      },
    }

    return {
      nodes: [],
      edges: [],
      sourceNodeCount: 0,
      selectedSourceNodeIds: [],
      toolbarSourceNodeId: null,
      previewNodeId: null,
      detailLevel: 'full',
      pendingFocusSourceNodeId: null,
      nodeAgentRuns: {},
      actions,
    }
  })
}

function agentEventMessage(event: AgentEvent) {
  const message = event.data?.message
  return typeof message === 'string' ? message : 'Agent 执行失败'
}

export const FlowNodeStoreContext = createContext<FlowNodeStoreApi | null>(null)

export function useFlowNodeStore<T>(selector: (state: FlowNodeStore) => T): T {
  return useStore(useFlowNodeStoreApi(), selector)
}

export function useFlowNodeStoreApi(): FlowNodeStoreApi {
  const store = useContext(FlowNodeStoreContext)
  if (store === null) throw new Error('useFlowNodeStore must be used within FlowNodeStoreProvider')
  return store
}

function mergeRemoteNodes(currentNodes: StoryFlowNode[], incomingNodes: StoryFlowNode[]) {
  const currentById = new Map(currentNodes.map((node) => [node.id, node]))
  let changed = currentNodes.length !== incomingNodes.length

  const nodes = incomingNodes.map((incomingNode, index) => {
    const currentNode = currentById.get(incomingNode.id)
    if (currentNode === undefined) {
      changed = true
      return incomingNode
    }

    if (isSameRemoteNode(currentNode, incomingNode)) {
      if (currentNodes[index] !== currentNode) changed = true
      return currentNode
    }

    changed = true
    return {
      ...currentNode,
      ...incomingNode,
      position: currentNode.dragging ? currentNode.position : incomingNode.position,
      selected: currentNode.selected,
    }
  })

  return changed ? nodes : currentNodes
}

function isSameRemoteNode(current: StoryFlowNode, incoming: StoryFlowNode) {
  return current.type === incoming.type
    && current.draggable === incoming.draggable
    && (current.dragging || isSameNodePosition(current, incoming))
    && isSameNodeData(current.data, incoming.data)
}

function isSameNodePosition(current: StoryFlowNode, incoming: StoryFlowNode) {
  return current.position.x === incoming.position.x && current.position.y === incoming.position.y
}

function isSameNodeData(current: FlowNodeData, incoming: FlowNodeData) {
  return current.workId === incoming.workId
    && current.sourceId === incoming.sourceId
    && current.sourceType === incoming.sourceType
    && current.kind === incoming.kind
    && current.title === incoming.title
    && current.content === incoming.content
    && current.revision === incoming.revision
    && current.layerId === incoming.layerId
    && current.archiveStateResolved === incoming.archiveStateResolved
    && current.archiveLocked === incoming.archiveLocked
    && current.archiveExpanded === incoming.archiveExpanded
    && current.archiveLayoutDisabled === incoming.archiveLayoutDisabled
    && current.archiveLayoutPending === incoming.archiveLayoutPending
    && current.onToggleArchive === incoming.onToggleArchive
    && current.onLayoutArchive === incoming.onLayoutArchive
    && areStringArraysEqual(current.contextTags, incoming.contextTags)
}

function areRemoteEdgesEqual(current: Edge[], incoming: Edge[]) {
  if (current.length !== incoming.length) return false
  for (let index = 0; index < current.length; index += 1) {
    const currentEdge = current[index]
    const incomingEdge = incoming[index]
    if (
      currentEdge.id !== incomingEdge.id
      || currentEdge.source !== incomingEdge.source
      || currentEdge.target !== incomingEdge.target
      || currentEdge.data?.kind !== incomingEdge.data?.kind
    ) {
      return false
    }
  }
  return true
}

function affectsSelection(change: Parameters<OnNodesChange<StoryFlowNode>>[0][number]) {
  return change.type === 'select' || change.type === 'add' || change.type === 'remove' || change.type === 'replace'
}

function collectSelectedSourceNodeIds(nodes: StoryFlowNode[]) {
  const selectedSourceNodeIds: string[] = []
  for (const node of nodes) {
    if (node.selected && node.data.sourceType === 'node') selectedSourceNodeIds.push(node.data.sourceId)
  }
  return selectedSourceNodeIds
}

function resolveToolbarSourceNodeId(currentNodeId: string | null, selectedNodeIds: string[]) {
  return selectedNodeIds.length === 1 && selectedNodeIds[0] === currentNodeId ? currentNodeId : null
}

function countSourceNodes(nodes: StoryFlowNode[]) {
  let count = 0
  for (const node of nodes) {
    if (node.data.sourceType === 'node') count += 1
  }
  return count
}

function selectOnlySourceNode(nodes: StoryFlowNode[], nodeId: string) {
  return nodes.map((node) => {
    const selected = node.data.sourceType === 'node' && node.data.sourceId === nodeId
    return node.selected === selected ? node : { ...node, selected }
  })
}

function areStringArraysEqual(left: string[], right: string[]) {
  if (left.length !== right.length) return false
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) return false
  }
  return true
}
