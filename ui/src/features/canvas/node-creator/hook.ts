import { useCallback, useEffect, useMemo } from 'react'

import {
  useCanvasHistory,
  useDeleteCanvasNodes,
  useDeleteCanvasEdges,
  useRedoCanvasAction,
  useUndoCanvasAction,
} from '@/apis/canvas-apis'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { isTextEntryTarget } from '@/features/canvas/keyboard'
import { useArchiveLocks } from '@/features/canvas/story-spine/use-archive-locks'

interface CanvasHistoryActions {
  canUndo: boolean
  canRedo: boolean
  undo: () => void
  redo: () => void
  undoLabel: string
  redoLabel: string
}

export function useCanvasHistoryActions(workId: string): CanvasHistoryActions {
  const archiveLocks = useArchiveLocks(workId)
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const edges = useFlowNodeStore((state) => state.edges)
  const selectedEdges = useMemo(() => edges.filter((edge) =>
    edge.selected && edge.data?.persisted === true), [edges])
  const selectedEdgeIds = useMemo(() => selectedEdges.map((edge) => edge.id), [selectedEdges])
  const history = useCanvasHistory(workId)
  const { mutate: undoMutation, isPending: isUndoPending } = useUndoCanvasAction(workId)
  const { mutate: redoMutation, isPending: isRedoPending } = useRedoCanvasAction(workId)
  const { mutate: deleteNodes, isPending: isDeletePending } = useDeleteCanvasNodes(workId)
  const { mutate: deleteEdges, isPending: isEdgeDeletePending } = useDeleteCanvasEdges(workId)
  const historyBusy = isUndoPending || isRedoPending
  const canUndo = history.data?.canUndo === true && !historyBusy
  const canRedo = history.data?.canRedo === true && !historyBusy

  const undo = useCallback(() => {
    if (canUndo) undoMutation()
  }, [canUndo, undoMutation])

  const redo = useCallback(() => {
    if (canRedo) redoMutation()
  }, [canRedo, redoMutation])

  const deleteSelectedNodes = useCallback(() => {
    if (selectedNodeIds.length === 0 || isDeletePending) return
    if (!archiveLocks.isResolved || selectedNodeIds.some((nodeId) => archiveLocks.lockedNodeIds.has(nodeId))) return
    if (selectedNodeIds.length > 1 && !window.confirm(`删除选中的 ${selectedNodeIds.length} 个节点？`)) return
    deleteNodes(selectedNodeIds)
  }, [archiveLocks.isResolved, archiveLocks.lockedNodeIds, deleteNodes, isDeletePending, selectedNodeIds])

  const deleteSelectedEdges = useCallback(() => {
    if (selectedEdgeIds.length === 0 || isEdgeDeletePending) return
    if (
      !archiveLocks.isResolved
      || selectedEdges.some((edge) => archiveLocks.lockedNodeIds.has(edge.target))
    ) return
    if (selectedEdgeIds.length > 1 && !window.confirm(`删除选中的 ${selectedEdgeIds.length} 条连接？`)) return
    deleteEdges(selectedEdgeIds)
  }, [archiveLocks.isResolved, archiveLocks.lockedNodeIds, deleteEdges, isEdgeDeletePending, selectedEdgeIds, selectedEdges])

  useEffect(() => {
    const handleShortcut = (event: KeyboardEvent) => {
      if (event.repeat || isTextEntryTarget(event.target)) return
      const key = event.key.toLowerCase()
      const commandModifier = event.metaKey || event.ctrlKey

      if (commandModifier && key === 'z') {
        if (event.shiftKey) {
          if (!canRedo) return
          event.preventDefault()
          redo()
          return
        }
        if (!canUndo) return
        event.preventDefault()
        undo()
        return
      }
      if (event.ctrlKey && !event.metaKey && key === 'y') {
        if (!canRedo) return
        event.preventDefault()
        redo()
        return
      }
      if (!commandModifier && !event.altKey && (key === 'delete' || key === 'backspace')) {
        if (selectedNodeIds.length === 0 && selectedEdgeIds.length === 0) return
        event.preventDefault()
        if (selectedNodeIds.length > 0) {
          if (!isDeletePending) deleteSelectedNodes()
          return
        }
        if (!isEdgeDeletePending) deleteSelectedEdges()
      }
    }

    window.addEventListener('keydown', handleShortcut)
    return () => window.removeEventListener('keydown', handleShortcut)
  }, [canRedo, canUndo, deleteSelectedEdges, deleteSelectedNodes, isDeletePending, isEdgeDeletePending, redo, selectedEdgeIds.length, selectedNodeIds.length, undo])

  return {
    canUndo,
    canRedo,
    undo,
    redo,
    undoLabel: history.data?.undoLabel ? `撤销：${history.data.undoLabel}` : '撤销',
    redoLabel: history.data?.redoLabel ? `重做：${history.data.redoLabel}` : '重做',
  }
}
