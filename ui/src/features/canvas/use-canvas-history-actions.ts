import { useCallback, useEffect } from 'react'

import {
  useCanvasHistory,
  useDeleteCanvasNodes,
  useRedoCanvasAction,
  useUndoCanvasAction,
} from '@/apis/canvas-apis'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { isTextEntryTarget } from '@/features/canvas/keyboard'

export function useCanvasHistoryActions(workId: string) {
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const history = useCanvasHistory(workId)
  const { mutate: undoMutation, isPending: isUndoPending } = useUndoCanvasAction(workId)
  const { mutate: redoMutation, isPending: isRedoPending } = useRedoCanvasAction(workId)
  const { mutate: deleteNodes, isPending: isDeletePending } = useDeleteCanvasNodes(workId)
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
    if (selectedNodeIds.length > 1 && !window.confirm(`删除选中的 ${selectedNodeIds.length} 个节点？`)) return
    deleteNodes(selectedNodeIds)
  }, [deleteNodes, isDeletePending, selectedNodeIds])

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
        if (selectedNodeIds.length === 0 || isDeletePending) return
        event.preventDefault()
        deleteSelectedNodes()
      }
    }

    window.addEventListener('keydown', handleShortcut)
    return () => window.removeEventListener('keydown', handleShortcut)
  }, [canRedo, canUndo, deleteSelectedNodes, isDeletePending, redo, selectedNodeIds.length, undo])

  return {
    canUndo,
    canRedo,
    undo,
    redo,
    undoLabel: history.data?.undoLabel ? `撤销：${history.data.undoLabel}` : '撤销',
    redoLabel: history.data?.redoLabel ? `重做：${history.data.redoLabel}` : '重做',
  }
}
