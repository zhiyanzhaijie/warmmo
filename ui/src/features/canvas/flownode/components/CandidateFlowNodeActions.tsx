import { Check, LoaderCircle, X } from 'lucide-react'
import { memo, type PointerEvent } from 'react'

import { useAcceptCanvasCandidate, useRejectCanvasCandidate } from '@/apis/canvas-apis'

interface CandidateFlowNodeActionsProps {
  workId: string
  candidateId: string
  title: string
}

export const CandidateFlowNodeActions = memo(function CandidateFlowNodeActions({
  workId,
  candidateId,
  title,
}: CandidateFlowNodeActionsProps) {
  const acceptCandidate = useAcceptCanvasCandidate(workId)
  const rejectCandidate = useRejectCanvasCandidate(workId)
  const pending = acceptCandidate.isPending || rejectCandidate.isPending
  const error = acceptCandidate.error ?? rejectCandidate.error

  return (
    <div
      className="nodrag nopan nowheel border-t border-hairline bg-canvas px-space-sm py-space-xs"
      onPointerDown={stopPointerPropagation}
    >
      <div className="flex items-center gap-space-xs">
        <button
          className="flex h-7 flex-1 items-center justify-center gap-1 rounded-sm bg-primary px-space-sm text-body-sm font-medium text-on-primary disabled:opacity-40"
          type="button"
          disabled={pending}
          onClick={() => acceptCandidate.mutate({ candidateId, title })}
        >
          {acceptCandidate.isPending
            ? <LoaderCircle className="animate-spin" size={13} />
            : <Check size={13} />}
          接受
        </button>
        <button
          className="flex h-7 flex-1 items-center justify-center gap-1 rounded-sm border border-hairline px-space-sm text-body-sm font-medium text-mute hover:text-ink disabled:opacity-40"
          type="button"
          disabled={pending}
          onClick={() => rejectCandidate.mutate(candidateId)}
        >
          {rejectCandidate.isPending
            ? <LoaderCircle className="animate-spin" size={13} />
            : <X size={13} />}
          丢弃
        </button>
      </div>
      {error instanceof Error ? (
        <p className="mt-space-xs line-clamp-2 text-body-sm text-error">{error.message}</p>
      ) : null}
    </div>
  )
})

function stopPointerPropagation(event: PointerEvent<HTMLDivElement>) {
  event.stopPropagation()
}
