import { LockKeyhole, Maximize2 } from 'lucide-react'
import { memo } from 'react'
import { useNavigate } from 'react-router-dom'

import { useCanvasNode } from '@/apis/canvas-apis'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { NodeDocument } from '@/features/canvas/node-detail/NodeDocument'
import { NodeVersionControl } from '@/features/canvas/node-detail/NodeVersionControl'
import { useArchiveLocks } from '@/features/canvas/story-spine/use-archive-locks'

export const NodeDetailSheet = memo(function NodeDetailSheet({ workId }: { workId: string }) {
  const navigate = useNavigate()
  const nodeId = useFlowNodeStore((state) => state.previewNodeId)
  const closePreview = useFlowNodeStore((state) => state.actions.closePreview)
  const nodeQuery = useCanvasNode(workId, nodeId)
  const archiveLocks = useArchiveLocks(workId)
  const isArchiveLocked = nodeId !== null && archiveLocks.lockedNodeIds.has(nodeId)
  const canMutateNode = archiveLocks.isResolved && !isArchiveLocked

  return (
    <Sheet
      open={nodeId !== null}
      onOpenChange={(open) => {
        if (!open) closePreview()
      }}
    >
      <SheetContent>
        <SheetHeader className="flex min-h-16 flex-wrap items-center justify-between gap-space-md py-space-sm">
          <div>
            <SheetTitle className="text-label-sm">节点预览</SheetTitle>
            <SheetDescription>完整内容只读视图</SheetDescription>
          </div>
          {nodeQuery.data !== undefined ? (
            <div className="mr-10 ml-auto flex min-w-0 items-center gap-space-sm">
              {!canMutateNode ? (
                <span className="inline-flex h-9 items-center gap-space-xs text-body-sm text-mute">
                  <LockKeyhole aria-hidden="true" size={14} />
                  {!archiveLocks.isResolved
                    ? archiveLocks.isError ? '无法确认归档状态' : '正在确认归档状态'
                    : '已归档锁定'}
                </span>
              ) : <NodeVersionControl workId={workId} node={nodeQuery.data} />}
              {canMutateNode ? <button
                className="flex h-9 shrink-0 items-center gap-space-xs rounded-sm border border-hairline px-space-sm text-button-md text-ink transition-colors hover:bg-hairline-soft"
                type="button"
                aria-label="全屏编辑"
                onClick={() => {
                  closePreview()
                  navigate(`/works/${workId}/nodes/${nodeQuery.data.id}/edit`, {
                    state: { fromCanvas: true },
                  })
                }}
              >
                <Maximize2 size={15} />
                <span className="hidden sm:inline">全屏编辑</span>
              </button> : null}
            </div>
          ) : null}
        </SheetHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-space-xl py-space-xl">
          {nodeQuery.isPending ? (
            <SheetStatus message="正在读取节点内容" />
          ) : nodeQuery.isError ? (
            <SheetStatus
              message={nodeQuery.error instanceof Error ? nodeQuery.error.message : '节点内容读取失败'}
              tone="error"
            />
          ) : nodeQuery.data !== undefined ? (
            <NodeDocument node={nodeQuery.data} mode="read" />
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
})

function SheetStatus({ message, tone = 'muted' }: { message: string; tone?: 'muted' | 'error' }) {
  return (
    <div className={`grid min-h-48 place-items-center text-body-md ${tone === 'error' ? 'text-error' : 'text-mute'}`}>
      {message}
    </div>
  )
}
