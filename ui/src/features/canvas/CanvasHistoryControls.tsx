import { Redo2, Trash2, Undo2 } from 'lucide-react'
import { memo, type ReactNode, useCallback, useEffect } from 'react'

import {
  useCanvasHistory,
  useDeleteCanvasNodes,
  useRedoCanvasAction,
  useUndoCanvasAction,
} from '@/apis/canvas-apis'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { isTextEntryTarget } from '@/features/canvas/keyboard'

export const CanvasHistoryControls = memo(function CanvasHistoryControls({ workId }: { workId: string }) {
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const history = useCanvasHistory(workId)
  const { mutate: undo, isPending: isUndoPending, error: undoError } = useUndoCanvasAction(workId)
  const { mutate: redo, isPending: isRedoPending, error: redoError } = useRedoCanvasAction(workId)
  const { mutate: deleteNodes, isPending: isDeletePending, error: deleteError } = useDeleteCanvasNodes(workId)
  const historyBusy = isUndoPending || isRedoPending
  const canUndo = history.data?.canUndo === true && !historyBusy
  const canRedo = history.data?.canRedo === true && !historyBusy

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

  const actionError = undoError ?? redoError ?? deleteError
  const undoLabel = history.data?.undoLabel ? `撤销：${history.data.undoLabel}` : '撤销'
  const redoLabel = history.data?.redoLabel ? `重做：${history.data.redoLabel}` : '重做'

  return (
    <div className="absolute bottom-space-md left-space-md z-20 flex max-w-[calc(100vw_-_2rem)] items-end gap-space-sm">
      <div
        aria-label="画布历史操作"
        className="flex h-9 shrink-0 items-center overflow-hidden rounded-sm border border-hairline bg-canvas-elevated shadow-floating"
        role="toolbar"
      >
        <ControlButton label={undoLabel} shortcut="⌘Z / Ctrl+Z" disabled={!canUndo} onClick={() => undo()}>
          <Undo2 size={15} />
        </ControlButton>
        <ControlButton label={redoLabel} shortcut="⇧⌘Z / Ctrl+Shift+Z" disabled={!canRedo} onClick={() => redo()}>
          <Redo2 size={15} />
        </ControlButton>
        <ControlButton
          label={selectedNodeIds.length > 1 ? `删除 ${selectedNodeIds.length} 个节点` : '删除节点'}
          shortcut="Delete / Backspace"
          disabled={selectedNodeIds.length === 0 || isDeletePending}
          destructive
          onClick={deleteSelectedNodes}
        >
          <Trash2 size={15} />
        </ControlButton>
      </div>
      {actionError !== null ? (
        <span
          className="max-w-72 truncate rounded-sm border border-error/25 bg-canvas-elevated px-space-sm py-space-xs text-body-sm text-error shadow-floating"
          role="status"
        >
          {actionError instanceof Error ? actionError.message : '画布操作失败'}
        </span>
      ) : null}
    </div>
  )
})

function ControlButton({
  children,
  label,
  shortcut,
  disabled,
  destructive = false,
  onClick,
}: {
  children: ReactNode
  label: string
  shortcut: string
  disabled: boolean
  destructive?: boolean
  onClick: () => void
}) {
  return (
    <button
      aria-label={label}
      className={`grid size-9 place-items-center border-l border-hairline text-mute transition-colors first:border-l-0 disabled:opacity-35 ${destructive ? 'hover:bg-error/5 hover:text-error' : 'hover:bg-hairline-soft hover:text-ink'}`}
      type="button"
      title={`${label} · ${shortcut}`}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  )
}
