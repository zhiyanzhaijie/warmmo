import { Archive, ArchiveRestore, ChevronRight, CircleDashed, History } from 'lucide-react'
import { memo, useState } from 'react'

import { useChapterArchiveHistory, useRetractChapterArchive } from '@/apis/chapter-archive-apis'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

const archiveDateFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

interface ArchiveHistorySheetProps {
  workId: string
  chapterNodeId: string | null
  chapterTitle: string
  onOpenChange: (open: boolean) => void
}

export const ArchiveHistorySheet = memo(function ArchiveHistorySheet({
  workId,
  chapterNodeId,
  chapterTitle,
  onOpenChange,
}: ArchiveHistorySheetProps) {
  const historyQuery = useChapterArchiveHistory(workId, chapterNodeId)
  const [selectedArchiveId, setSelectedArchiveId] = useState<string | null>(null)
  const [isConfirmingRetraction, setIsConfirmingRetraction] = useState(false)
  const retractMutation = useRetractChapterArchive(workId)
  const archives = historyQuery.data ?? []
  const selectedArchive = archives.find((archive) => archive.id === selectedArchiveId)
    ?? archives.find((archive) => archive.isCurrent)
    ?? archives.at(-1)

  const canRetract = selectedArchive?.isCurrent === true && selectedArchive.retractedAt === undefined

  return (
    <Sheet
      open={chapterNodeId !== null}
      onOpenChange={(open) => {
        if (!open) {
          setIsConfirmingRetraction(false)
          retractMutation.reset()
        }
        onOpenChange(open)
      }}
    >
      <SheetContent className="w-[min(52rem,calc(100vw-1rem))]">
        <SheetHeader>
          <div className="flex items-center gap-space-xs text-mute">
            <History aria-hidden="true" size={14} />
            <span className="font-mono text-mono-eyebrow">ARCHIVE HISTORY</span>
          </div>
          <SheetTitle className="mt-space-xs">{chapterTitle}</SheetTitle>
          <SheetDescription>{archives.length} 个归档修订记录</SheetDescription>
        </SheetHeader>

        {historyQuery.isPending ? (
          <HistoryStatus message="正在读取章节归档" />
        ) : historyQuery.isError ? (
          <HistoryStatus message={historyQuery.error instanceof Error ? historyQuery.error.message : '章节归档读取失败'} tone="error" />
        ) : selectedArchive === undefined ? (
          <HistoryStatus message="当前章节还没有归档记录" />
        ) : (
          <div className="grid min-h-0 flex-1 grid-cols-[9rem_minmax(0,1fr)] sm:grid-cols-[12rem_minmax(0,1fr)]">
            <nav aria-label="归档修订" className="overflow-y-auto border-r border-hairline py-space-xs">
              {archives.toReversed().map((archive) => (
                <button
                  key={archive.id}
                  type="button"
                  aria-current={selectedArchive.id === archive.id ? 'true' : undefined}
                  className="flex w-full flex-col border-l-2 border-transparent px-space-sm py-space-sm text-left hover:bg-hairline-soft aria-current:border-link aria-current:bg-hairline-soft"
                  onClick={() => {
                    setSelectedArchiveId(archive.id)
                    setIsConfirmingRetraction(false)
                    retractMutation.reset()
                  }}
                >
                  <span className="flex items-center gap-1 text-label-sm text-ink">
                    归档 v{archive.revision}
                    {archive.isCurrent ? <span className="font-mono text-[0.625rem] text-mute">CURRENT</span> : null}
                    {archive.retractedAt ? <span className="font-mono text-[0.625rem] text-error">RETRACTED</span> : null}
                  </span>
                  <time className="mt-1 text-body-sm text-faint" dateTime={archive.createdAt}>
                    {archiveDateFormatter.format(new Date(archive.createdAt))}
                  </time>
                </button>
              ))}
            </nav>

            <article className="min-w-0 overflow-y-auto px-space-lg py-space-lg sm:px-space-xl">
              <header className="border-b border-hairline pb-space-lg">
                <div className="flex flex-wrap items-center gap-space-xs">
                  <Archive aria-hidden="true" className="text-mute" size={15} />
                  <h3 className="text-heading-md text-ink">归档 v{selectedArchive.revision}</h3>
                  {selectedArchive.retractedAt ? (
                    <span className="font-mono text-[0.625rem] text-error">RETRACTED</span>
                  ) : null}
                  {selectedArchive.projectionStatus === 'pending' ? (
                    <span className="inline-flex items-center gap-1 text-body-sm text-warning">
                      <CircleDashed aria-hidden="true" size={12} />
                      检索投影等待同步
                    </span>
                  ) : null}
                </div>
                <p className="mt-space-sm whitespace-pre-wrap text-body-md leading-6 text-body">{selectedArchive.summary}</p>
                <div className="mt-space-sm flex flex-wrap gap-x-space-lg gap-y-1 font-mono text-[0.625rem] text-faint">
                  <span>OUTLINE REVISION {selectedArchive.outlineRevision}</span>
                  <span>{selectedArchive.sections.length} SECTIONS</span>
                </div>
                {canRetract ? (
                  <div className="mt-space-md flex flex-wrap items-center gap-space-sm">
                    {isConfirmingRetraction ? (
                      <>
                        <span className="text-body-sm text-warning">撤销后章节与小节将恢复编辑，历史记录仍保留。</span>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          disabled={retractMutation.isPending}
                          onClick={() => setIsConfirmingRetraction(false)}
                        >
                          取消
                        </Button>
                        <Button
                          type="button"
                          variant="destructive"
                          size="sm"
                          disabled={retractMutation.isPending}
                          onClick={() => retractMutation.mutate(selectedArchive.id, { onSuccess: () => setIsConfirmingRetraction(false) })}
                        >
                          <ArchiveRestore aria-hidden="true" size={14} />
                          {retractMutation.isPending ? '撤销中...' : '确认撤销'}
                        </Button>
                      </>
                    ) : (
                      <Button type="button" variant="outline" size="sm" onClick={() => setIsConfirmingRetraction(true)}>
                        <ArchiveRestore aria-hidden="true" size={14} />
                        撤销归档
                      </Button>
                    )}
                  </div>
                ) : null}
                {retractMutation.isError ? (
                  <p className="mt-space-sm text-body-sm text-error">
                    {retractMutation.error instanceof Error ? retractMutation.error.message : '撤销归档失败'}
                  </p>
                ) : null}
              </header>

              <div className="divide-y divide-hairline">
                {selectedArchive.sections.map((section) => (
                  <details key={`${selectedArchive.id}:${section.chapterSectionNodeId}`} className="group py-space-md">
                    <summary className="flex cursor-pointer list-none items-start gap-space-sm">
                      <span className="grid size-6 shrink-0 place-items-center bg-hairline-soft font-mono text-body-sm text-mute">
                        {section.ordinal}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block text-label-sm text-ink">{section.title}</span>
                        <span className="mt-1 block text-body-sm leading-5 text-mute">{section.summary}</span>
                      </span>
                      <span className="shrink-0 font-mono text-[0.625rem] text-faint">R{section.nodeRevision}</span>
                      <ChevronRight aria-hidden="true" className="mt-0.5 shrink-0 text-faint transition-transform group-open:rotate-90" size={14} />
                    </summary>
                    <div className="mt-space-md border-l border-hairline pl-space-lg whitespace-pre-wrap text-body-md leading-6 text-body">
                      {section.content}
                    </div>
                  </details>
                ))}
              </div>
            </article>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
})

function HistoryStatus({ message, tone = 'muted' }: { message: string; tone?: 'muted' | 'error' }) {
  return (
    <div className={`grid min-h-0 flex-1 place-items-center px-space-xl text-body-md ${tone === 'error' ? 'text-error' : 'text-mute'}`}>
      {message}
    </div>
  )
}
