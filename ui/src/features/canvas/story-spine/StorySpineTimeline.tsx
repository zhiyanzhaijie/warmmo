import { Archive, CircleDashed, History } from 'lucide-react'
import { memo, useCallback, useMemo, useState } from 'react'

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { useFocusNode } from '@/features/canvas/flownode/use-focus-node'
import { ArchiveHistorySheet } from '@/features/canvas/story-spine/ArchiveHistorySheet'
import { toStorySpineChapters } from '@/features/canvas/story-spine/adapters'
import type { StorySpineChapter, StorySpineSection, StorySpineTone } from '@/features/canvas/story-spine/types'
import type { CanvasNode } from '@/types/canvas'
import type { ChapterArchive } from '@/types/chapter-archive'

interface StorySpineTimelineProps {
  workId: string
  archives: ChapterArchive[]
  canvasNodes: CanvasNode[]
  expandedChapterNodeIds: ReadonlySet<string>
  onExpandChapter: (chapterNodeId: string) => void
}

const toneClasses: Record<StorySpineTone, { rail: string; active: string }> = {
  cyan: { rail: 'bg-cyan/55', active: 'bg-cyan text-primary' },
  magenta: { rail: 'bg-magenta/50', active: 'bg-magenta text-white' },
  amber: { rail: 'bg-warning/55', active: 'bg-warning text-primary' },
  violet: { rail: 'bg-violet/45', active: 'bg-violet text-white' },
  green: { rail: 'bg-emerald-500/45', active: 'bg-emerald-500 text-white' },
}

export const StorySpineTimeline = memo(function StorySpineTimeline({
  workId,
  archives,
  canvasNodes,
  expandedChapterNodeIds,
  onExpandChapter,
}: StorySpineTimelineProps) {
  const chapters = useMemo(
    () => toStorySpineChapters(archives, canvasNodes),
    [archives, canvasNodes],
  )
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const selectedNodeId = selectedNodeIds.length === 1 ? selectedNodeIds[0] : null
  const focusNode = useFocusNode()
  const focusSection = useCallback((chapterNodeId: string, sectionNodeId: string, needsReveal: boolean) => {
    if (needsReveal) onExpandChapter(chapterNodeId)
    focusNode(sectionNodeId)
    if (needsReveal) window.requestAnimationFrame(() => focusNode(sectionNodeId))
  }, [focusNode, onExpandChapter])
  const [historyChapterNodeId, setHistoryChapterNodeId] = useState<string | null>(null)
  const historyChapter = chapters.find((chapter) => chapter.nodeId === historyChapterNodeId)

  if (chapters.length === 0) return null

  return (
    <>
      <section
        aria-label="故事脉络"
        className="fixed right-space-md bottom-14 left-space-md z-30 h-[5.75rem] overflow-hidden rounded-sm border border-hairline bg-canvas-elevated/95 shadow-floating backdrop-blur-sm sm:right-space-md sm:bottom-space-md sm:left-44"
      >
        <div className="flex h-7 items-center gap-space-xs border-b border-hairline px-space-sm">
          <Archive aria-hidden="true" className="text-mute" size={13} />
          <h2 className="text-label-sm text-ink">故事脉络</h2>
          <span className="font-mono text-body-sm text-faint">{chapters.length}</span>
        </div>
        <TooltipProvider delayDuration={180}>
          <div className="h-[4rem] overflow-x-auto overflow-y-hidden px-space-sm py-space-xs">
            <div className="flex h-full min-w-max items-stretch gap-1">
              {chapters.map((chapter) => (
                <ChapterTrack
                  key={chapter.archiveId}
                  chapter={chapter}
                  expanded={expandedChapterNodeIds.has(chapter.nodeId)}
                  selectedNodeId={selectedNodeId}
                  onFocusNode={focusNode}
                  onFocusSection={focusSection}
                  onOpenHistory={setHistoryChapterNodeId}
                />
              ))}
            </div>
          </div>
        </TooltipProvider>
      </section>
      <ArchiveHistorySheet
        workId={workId}
        chapterNodeId={historyChapterNodeId}
        chapterTitle={historyChapter?.title ?? ''}
        onOpenChange={(open) => {
          if (!open) setHistoryChapterNodeId(null)
        }}
      />
    </>
  )
})

interface ChapterTrackProps {
  chapter: StorySpineChapter
  expanded: boolean
  selectedNodeId: string | null
  onFocusNode: (nodeId: string) => void
  onFocusSection: (chapterNodeId: string, sectionNodeId: string, needsReveal: boolean) => void
  onOpenHistory: (nodeId: string) => void
}

const ChapterTrack = memo(function ChapterTrack({
  chapter,
  expanded,
  selectedNodeId,
  onFocusNode,
  onFocusSection,
  onOpenHistory,
}: ChapterTrackProps) {
  const tone = toneClasses[chapter.tone]

  return (
    <div className="flex min-w-36 flex-col">
      <div className={`flex h-5 items-center border-l-2 px-1.5 ${tone.rail}`}>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              aria-disabled={!chapter.isNodeAvailable}
              className="min-w-0 flex-1 truncate text-left text-body-sm font-medium text-ink aria-disabled:cursor-not-allowed aria-disabled:opacity-45"
              onClick={() => {
                if (chapter.isNodeAvailable) onFocusNode(chapter.nodeId)
              }}
            >
              {chapter.title}
            </button>
          </TooltipTrigger>
          <TooltipContent side="top" align="start" className="max-w-72">
            <p className="text-label-sm">{chapter.title} · 归档 v{chapter.archiveRevision}</p>
            <p className="mt-1 text-body-sm text-mute">{chapter.summary}</p>
            {!chapter.isNodeAvailable ? <p className="mt-1 text-body-sm text-warning">画布节点已不存在</p> : null}
          </TooltipContent>
        </Tooltip>
        {chapter.isProjectionPending ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="ml-1 grid size-4 shrink-0 place-items-center text-warning" tabIndex={0}>
                <CircleDashed aria-hidden="true" size={11} />
                <span className="sr-only">检索投影等待同步</span>
              </span>
            </TooltipTrigger>
            <TooltipContent side="top">数据库归档可用，检索投影等待同步</TooltipContent>
          </Tooltip>
        ) : null}
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              aria-label={`查看${chapter.title}归档历史`}
              className="ml-0.5 grid size-4 shrink-0 place-items-center text-mute hover:text-ink"
              onClick={() => onOpenHistory(chapter.nodeId)}
            >
              <History aria-hidden="true" size={11} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="top">查看归档历史</TooltipContent>
        </Tooltip>
      </div>
      <div className={`flex h-7 items-stretch gap-px p-0.5 ${tone.rail}`}>
        {chapter.sections.map((section) => (
          <SectionTick
            key={section.id}
            chapterNodeId={chapter.nodeId}
            expanded={expanded}
            section={section}
            tone={chapter.tone}
            selected={selectedNodeId === section.nodeId}
            onFocusSection={onFocusSection}
          />
        ))}
      </div>
    </div>
  )
})

interface SectionTickProps {
  chapterNodeId: string
  expanded: boolean
  section: StorySpineSection
  tone: StorySpineTone
  selected: boolean
  onFocusSection: (chapterNodeId: string, sectionNodeId: string, needsReveal: boolean) => void
}

const SectionTick = memo(function SectionTick({
  chapterNodeId,
  expanded,
  section,
  tone,
  selected,
  onFocusSection,
}: SectionTickProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-disabled={!section.isNodeAvailable}
          aria-label={`定位到第 ${section.ordinal} 小节：${section.title}`}
          aria-pressed={selected}
          className={`grid h-6 w-9 shrink-0 place-items-center border border-white/30 font-mono text-[0.625rem] transition-[filter,transform] hover:brightness-110 active:scale-95 aria-disabled:cursor-not-allowed aria-disabled:opacity-35 ${selected ? toneClasses[tone].active : 'bg-canvas-elevated/85 text-body'}`}
          onClick={() => {
            if (section.isNodeAvailable) onFocusSection(chapterNodeId, section.nodeId, !expanded)
          }}
        >
          {section.ordinal}
        </button>
      </TooltipTrigger>
      <TooltipContent side="top" align="start" className="max-w-72">
        <p className="text-label-sm">{section.ordinal}. {section.title}</p>
        <p className="mt-1 text-body-sm text-mute">{section.summary}</p>
        <p className="mt-1 font-mono text-[0.625rem] text-faint">NODE REVISION {section.nodeRevision}</p>
        {!section.isNodeAvailable ? <p className="mt-1 text-body-sm text-warning">画布节点已不存在</p> : null}
      </TooltipContent>
    </Tooltip>
  )
})
