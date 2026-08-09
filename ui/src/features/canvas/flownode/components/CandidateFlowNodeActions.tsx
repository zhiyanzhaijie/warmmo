import { Check, FileText, LoaderCircle, X } from 'lucide-react'
import { memo, type PointerEvent, useState } from 'react'

import { useAcceptCanvasCandidate, useRejectCanvasCandidate } from '@/apis/canvas-apis'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { CanvasNodeKind } from '@/features/canvas/nodes/definitions'

interface CandidateFlowNodeActionsProps {
  workId: string
	candidateId: string
	kind: CanvasNodeKind
	title: string
	content: string
	reason?: string
}

export const CandidateFlowNodeActions = memo(function CandidateFlowNodeActions({
  workId,
	candidateId,
	kind,
	title,
	content,
	reason,
}: CandidateFlowNodeActionsProps) {
	const [detailsOpen, setDetailsOpen] = useState(false)
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
			<Button
				className="h-7 shrink-0 px-space-xs"
				disabled={pending}
				size="icon-sm"
				variant="ghost"
				aria-label="查看候选详情"
				onClick={() => setDetailsOpen(true)}
			>
				<FileText aria-hidden="true" size={14} />
				<span className="sr-only">查看候选详情</span>
			</Button>
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
		<Dialog open={detailsOpen} onOpenChange={setDetailsOpen}>
			<DialogContent className="max-h-[80vh] max-w-2xl overflow-y-auto">
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
					<DialogDescription>{kind} · Candidate 详情</DialogDescription>
				</DialogHeader>
				<div className="space-y-space-md">
					<div className="whitespace-pre-wrap text-body-md leading-7 text-ink">{content}</div>
					{reason ? <p className="border-l border-link/40 pl-space-sm text-body-sm leading-5 text-body">生成原因：{reason}</p> : null}
				</div>
				<DialogFooter>
					<Button disabled={pending} variant="outline" onClick={() => rejectCandidate.mutate(candidateId)}>
						{rejectCandidate.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <X size={14} />}
						丢弃
					</Button>
					<Button disabled={pending} onClick={() => acceptCandidate.mutate({ candidateId, title })}>
						{acceptCandidate.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <Check size={14} />}
						接受
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	</div>
	)
})

function stopPointerPropagation(event: PointerEvent<HTMLDivElement>) {
  event.stopPropagation()
}
