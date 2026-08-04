import { memo } from 'react'

import { useFlowNodeStore } from '@/features/canvas/flownode/store'

export const CanvasHeader = memo(function CanvasHeader({ workId }: { workId: string }) {
  const nodeCount = useFlowNodeStore((state) => state.sourceNodeCount)
  const selectedNodeCount = useFlowNodeStore((state) => state.selectedSourceNodeIds.length)

  return (
    <header className="absolute inset-x-0 top-0 z-20 flex h-16 items-center border-b border-hairline bg-canvas-elevated/95 px-16 backdrop-blur-sm">
      <div className="min-w-0">
        <div className="font-mono text-mono-eyebrow text-mute">WARMNOTE / {workId}</div>
        <div className="truncate text-label-sm">{nodeCount} 节点 · {selectedNodeCount} 已选</div>
      </div>
    </header>
  )
})
