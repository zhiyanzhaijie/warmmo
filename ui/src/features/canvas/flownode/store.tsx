import { applyEdgeChanges, applyNodeChanges, type Edge, type OnEdgesChange, type OnNodesChange } from '@xyflow/react'
import { createContext, useContext } from 'react'
import { useStore } from 'zustand'
import { createStore, type StoreApi } from 'zustand/vanilla'

import type { FlowNodeData, FlowNodeDetailLevel, StoryFlowNode } from '@/features/canvas/flownode/types'

interface FlowNodeStoreActions {
  syncNodes: (nodes: StoryFlowNode[]) => void
  syncEdges: (edges: Edge[]) => void
  onNodesChange: OnNodesChange<StoryFlowNode>
  onEdgesChange: OnEdgesChange<Edge>
  setDetailLevel: (detailLevel: FlowNodeDetailLevel) => void
  showNodeToolbar: (nodeId: string) => void
  hideNodeToolbar: () => void
  openPreview: (nodeId: string) => void
  closePreview: () => void
}

export interface FlowNodeStore {
  nodes: StoryFlowNode[]
  edges: Edge[]
  sourceNodeCount: number
  selectedSourceNodeIds: string[]
  toolbarSourceNodeId: string | null
  previewNodeId: string | null
  detailLevel: FlowNodeDetailLevel
  actions: FlowNodeStoreActions
}

export type FlowNodeStoreApi = StoreApi<FlowNodeStore>

export function createFlowNodeStore(): FlowNodeStoreApi {
  return createStore<FlowNodeStore>()((set) => {
    const actions: FlowNodeStoreActions = {
      syncNodes: (incomingNodes) => {
        set((state) => {
          const nodes = mergeRemoteNodes(state.nodes, incomingNodes)
          const sourceNodeCount = countSourceNodes(nodes)
          const selectedSourceNodeIds = collectSelectedSourceNodeIds(nodes)
          return {
            nodes,
            sourceNodeCount,
            selectedSourceNodeIds: areStringArraysEqual(selectedSourceNodeIds, state.selectedSourceNodeIds)
              ? state.selectedSourceNodeIds
              : selectedSourceNodeIds,
            toolbarSourceNodeId: resolveToolbarSourceNodeId(state.toolbarSourceNodeId, selectedSourceNodeIds),
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
      setDetailLevel: (detailLevel) => {
        set((state) => state.detailLevel === detailLevel ? state : { detailLevel })
      },
      showNodeToolbar: (nodeId) => {
        set((state) => state.selectedSourceNodeIds.length === 1 && state.selectedSourceNodeIds[0] === nodeId
          ? { toolbarSourceNodeId: nodeId }
          : state)
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
    }

    return {
      nodes: [],
      edges: [],
      sourceNodeCount: 0,
      selectedSourceNodeIds: [],
      toolbarSourceNodeId: null,
      previewNodeId: null,
      detailLevel: 'full',
      actions,
    }
  })
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

function areStringArraysEqual(left: string[], right: string[]) {
  if (left.length !== right.length) return false
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) return false
  }
  return true
}
