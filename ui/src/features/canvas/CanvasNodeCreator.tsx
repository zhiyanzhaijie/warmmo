import { useReactFlow } from '@xyflow/react'
import { LoaderCircle, Redo2, Undo2 } from 'lucide-react'
import { memo, useCallback, useEffect, useState, type ReactNode } from 'react'

import { useCreateCanvasNode } from '@/apis/canvas-apis'
import { isTextEntryTarget } from '@/features/canvas/keyboard'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'
import { creatableNodeKinds, nodeDefinitions } from '@/features/canvas/nodes/definitions'
import { useCanvasHistoryActions } from '@/features/canvas/use-canvas-history-actions'
import type { CanvasNodeKind } from '@/types/canvas'

export const CanvasNodeCreator = memo(function CanvasNodeCreator({ workId }: { workId: string }) {
  const flow = useReactFlow<StoryFlowNode>()
  const createNode = useCreateCanvasNode(workId)
  const history = useCanvasHistoryActions(workId)
  const [pendingKind, setPendingKind] = useState<CanvasNodeKind | null>(null)

  const createAtViewportCenter = useCallback((kind: CanvasNodeKind) => {
    if (createNode.isPending) return
    const definition = nodeDefinitions[kind]
    const cascadeOffset = (flow.getNodes().length % 6) * 24
    const viewportCenter = flow.screenToFlowPosition({
      x: window.innerWidth / 2,
      y: window.innerHeight / 2,
    })
    setPendingKind(kind)
    createNode.mutate({
      kind,
      title: `未命名${definition.label}`,
      content: '',
      x: viewportCenter.x + cascadeOffset,
      y: viewportCenter.y + cascadeOffset,
    }, {
      onSettled: () => setPendingKind(null),
    })
  }, [createNode, flow])

  useEffect(() => {
    const handleShortcut = (event: KeyboardEvent) => {
      if (event.repeat || event.metaKey || event.ctrlKey || event.altKey || isTextEntryTarget(event.target)) return
      const shortcut = Number(event.key)
      const kind = creatableNodeKinds.find(
        (candidateKind) => nodeDefinitions[candidateKind].shortcut === shortcut,
      )
      if (kind === undefined) return
      event.preventDefault()
      createAtViewportCenter(kind)
    }

    window.addEventListener('keydown', handleShortcut)
    return () => window.removeEventListener('keydown', handleShortcut)
  }, [createAtViewportCenter])

  return (
    <div className="fixed top-[4.5rem] left-1/2 z-30 flex h-11 max-w-[calc(100vw_-_2rem)] -translate-x-1/2 items-center gap-space-xs lg:top-space-md">
      <HistoryButton
        available={history.canUndo}
        label={history.undoLabel}
        shortcut="⌘Z / Ctrl+Z"
        onClick={history.undo}
      >
        <Undo2 size={15} />
      </HistoryButton>
      <nav
        aria-label="创建画布节点"
        className="flex h-11 min-w-0 items-center overflow-x-auto rounded-sm border border-hairline bg-canvas-elevated/95 p-1 backdrop-blur-sm"
      >
        {creatableNodeKinds.map((kind, index) => {
          const definition = nodeDefinitions[kind]
          const previousKind = creatableNodeKinds[index - 1]
          const startsCategory = previousKind !== undefined
            && nodeDefinitions[previousKind].category !== definition.category
          const Icon = definition.icon

          return (
            <div key={kind} className="flex items-center">
              {startsCategory ? <div className="mx-1 h-5 w-px bg-hairline" /> : null}
              <button
                aria-label={`新建${definition.label}`}
                className="relative grid size-8 shrink-0 place-items-center rounded-sm text-mute hover:bg-hairline-soft hover:text-ink disabled:cursor-wait disabled:opacity-40"
                type="button"
                title={`新建${definition.label} · 快捷键 ${definition.shortcut}`}
                disabled={createNode.isPending}
                onClick={() => createAtViewportCenter(kind)}
              >
                {pendingKind === kind
                  ? <LoaderCircle className="animate-spin" size={15} />
                  : <Icon size={15} />}
                <span className="pointer-events-none absolute right-0.5 bottom-0.5 font-mono text-[9px] leading-none text-faint">
                  {definition.shortcut}
                </span>
              </button>
            </div>
          )
        })}
      </nav>
      <HistoryButton
        available={history.canRedo}
        label={history.redoLabel}
        shortcut="⇧⌘Z / Ctrl+Shift+Z"
        onClick={history.redo}
      >
        <Redo2 size={15} />
      </HistoryButton>
    </div>
  )
})

function HistoryButton({
  available,
  children,
  label,
  shortcut,
  onClick,
}: {
  available: boolean
  children: ReactNode
  label: string
  shortcut: string
  onClick: () => void
}) {
  if (!available) return <span aria-hidden="true" className="size-8 shrink-0" />

  return (
    <button
      aria-label={label}
      className="grid size-8 shrink-0 place-items-center rounded-sm border-0 bg-transparent text-mute transition-colors hover:bg-hairline-soft hover:text-ink"
      title={`${label} · ${shortcut}`}
      type="button"
      onClick={onClick}
    >
      {children}
    </button>
  )
}
