import { memo, useState } from 'react'

import { useWork } from '@/apis/work-apis'
import { WorkEditorDialog } from '@/components/work/WorkEditorDialog'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'

export const CanvasHeader = memo(function CanvasHeader({ workId }: { workId: string }) {
  const work = useWork(workId)
  const [editorOpen, setEditorOpen] = useState(false)
  const nodeCount = useFlowNodeStore((state) => state.sourceNodeCount)
  const selectedNodeCount = useFlowNodeStore((state) => state.selectedSourceNodeIds.length)

  return (
    <header className="fixed top-space-md left-16 z-40 flex h-10 max-w-[calc(100vw_-_5rem)] items-center gap-space-sm rounded-sm border border-hairline bg-canvas-elevated/95 px-space-sm shadow-floating backdrop-blur-sm">
      <div className="min-w-0 max-w-48">
        <button className="block max-w-full cursor-pointer truncate rounded-sm px-space-xxs text-left text-label-sm text-ink hover:bg-hairline-soft" type="button" title={work.data?.title ?? workId} disabled={work.data === undefined} onClick={() => setEditorOpen(true)}>
          {work.isError ? workId : work.data?.title ?? '正在加载'}
        </button>
      </div>
      <span className="h-4 w-px shrink-0 bg-hairline" aria-hidden="true" />
      <span className="shrink-0 font-mono text-body-sm text-mute">{nodeCount}</span>
      <span className="hidden shrink-0 text-body-sm text-mute sm:inline">节点</span>
      {selectedNodeCount > 0 ? (
        <>
          <span className="h-4 w-px shrink-0 bg-hairline" aria-hidden="true" />
          <span className="shrink-0 font-mono text-body-sm text-link">{selectedNodeCount}</span>
          <span className="hidden shrink-0 text-body-sm text-mute sm:inline">已选</span>
        </>
      ) : null}
      <WorkEditorDialog open={editorOpen} work={work.data} onOpenChange={setEditorOpen} />
    </header>
  )
})
