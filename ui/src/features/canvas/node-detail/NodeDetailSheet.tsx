import { Maximize2 } from 'lucide-react'
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

export const NodeDetailSheet = memo(function NodeDetailSheet({ workId }: { workId: string }) {
  const navigate = useNavigate()
  const nodeId = useFlowNodeStore((state) => state.previewNodeId)
  const closePreview = useFlowNodeStore((state) => state.actions.closePreview)
  const nodeQuery = useCanvasNode(workId, nodeId)

  return (
    <Sheet
      open={nodeId !== null}
      onOpenChange={(open) => {
        if (!open) closePreview()
      }}
    >
      <SheetContent>
        <SheetHeader className="flex min-h-16 items-center justify-between gap-space-md py-space-sm">
          <div>
            <SheetTitle className="text-label-sm">节点预览</SheetTitle>
            <SheetDescription>完整内容只读视图</SheetDescription>
          </div>
          {nodeQuery.data !== undefined ? (
            <button
              className="mr-10 flex h-9 items-center gap-space-xs rounded-sm border border-hairline px-space-sm text-button-md text-ink transition-colors hover:bg-hairline-soft"
              type="button"
              onClick={() => {
                closePreview()
                navigate(`/works/${workId}/nodes/${nodeQuery.data.id}/edit`)
              }}
            >
              <Maximize2 size={15} />
              全屏编辑
            </button>
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
