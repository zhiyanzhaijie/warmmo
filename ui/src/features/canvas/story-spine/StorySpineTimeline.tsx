import { Archive, CircleDashed, History } from 'lucide-react'
import { memo, useCallback, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from '@/components/ui/drawer'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { useFocusNode } from '@/features/canvas/flownode/use-focus-node'
import { ArchiveHistorySheet } from '@/features/canvas/story-spine/ArchiveHistorySheet'
import { toStorySpineChapters } from '@/features/canvas/story-spine/adapters'
import type { StorySpineChapter, StorySpineSection, StorySpineTone } from '@/features/canvas/story-spine/types'
import type { CanvasNode } from '@/types/canvas'
import type { ChapterArchiveTimeline } from '@/types/chapter-archive'

interface StorySpineTimelineProps {
  workId: string
  archives: ChapterArchiveTimeline[]
  canvasNodes: CanvasNode[]
  expandedChapterNodeIds: ReadonlySet<string>
  onExpandChapter: (chapterNodeId: string) => void
}

const toneClasses: Record<StorySpineTone, { marker: string; highlight: string; selected: string }> = {
  cyan: { marker: 'bg-cyan', highlight: 'hover:border-cyan focus-visible:border-cyan', selected: 'border-cyan' },
  magenta: { marker: 'bg-magenta', highlight: 'hover:border-magenta focus-visible:border-magenta', selected: 'border-magenta' },
  amber: { marker: 'bg-warning', highlight: 'hover:border-warning focus-visible:border-warning', selected: 'border-warning' },
  violet: { marker: 'bg-violet', highlight: 'hover:border-violet focus-visible:border-violet', selected: 'border-violet' },
  green: { marker: 'bg-emerald-500', highlight: 'hover:border-emerald-500 focus-visible:border-emerald-500', selected: 'border-emerald-500' },
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
  const totalSectionUnits = useMemo(
    () => chapters.reduce((total, chapter) => total + Math.max(1, chapter.sections.length), 0),
    [chapters],
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
  const [isStorySpineOpen, setIsStorySpineOpen] = useState(false)
  const historyChapter = chapters.find((chapter) => chapter.nodeId === historyChapterNodeId)

  return (
    <>
      <Drawer open={isStorySpineOpen} onOpenChange={setIsStorySpineOpen}>
        <div className="pointer-events-none fixed right-space-md bottom-space-md left-space-md z-30 flex justify-center">
          <DrawerTrigger asChild>
            <Button
              aria-label="打开故事脉络"
              className="pointer-events-auto gap-space-xs border border-hairline bg-canvas-elevated/95 text-mute shadow-floating backdrop-blur-sm hover:bg-hairline-soft hover:text-ink"
              size="sm"
              variant="outline"
            >
              <Archive aria-hidden="true" size={14} />
              <span>故事脉络</span>
              <span className="font-mono text-body-sm text-faint">{chapters.length}</span>
            </Button>
          </DrawerTrigger>
        </div>
        <DrawerContent className="gap-0 border-t-0 [&>button:last-child]:top-1 [&>button:last-child]:right-space-sm">
          <DrawerHeader className="flex h-10 items-center gap-space-xs border-b-0 px-space-md py-0 pr-14">
            <Archive aria-hidden="true" className="text-mute" size={15} />
            <DrawerTitle className="text-label-sm">故事脉络</DrawerTitle>
            <span className="font-mono text-body-sm text-faint">{chapters.length}</span>
            <DrawerDescription className="sr-only">
              按章节查看故事脉络并定位到画布节点
            </DrawerDescription>
          </DrawerHeader>
          <TooltipProvider delayDuration={180}>
            <div className="overflow-x-auto overflow-y-hidden px-space-md py-space-sm">
              <div
                className="grid min-h-28 w-full min-w-[48rem] items-stretch"
                style={{ gridTemplateColumns: `repeat(${Math.max(1, totalSectionUnits)}, minmax(0, 1fr))` }}
              >
                {chapters.length === 0 ? (
                  <div className="col-[1/-1] flex min-w-64 items-center text-body-sm text-faint">暂无归档脉络</div>
                ) : chapters.map((chapter) => (
                    <ChapterTrack
                      key={chapter.archiveId}
                      chapter={chapter}
                      sectionUnits={Math.max(1, chapter.sections.length)}
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
        </DrawerContent>
      </Drawer>
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
  sectionUnits: number
  expanded: boolean
  selectedNodeId: string | null
  onFocusNode: (nodeId: string) => void
  onFocusSection: (chapterNodeId: string, sectionNodeId: string, needsReveal: boolean) => void
  onOpenHistory: (nodeId: string) => void
}

const ChapterTrack = memo(function ChapterTrack({
  chapter,
  sectionUnits,
  expanded,
  selectedNodeId,
  onFocusNode,
  onFocusSection,
  onOpenHistory,
}: ChapterTrackProps) {
  return (
    <div
      className="flex min-w-0 flex-col bg-transparent px-px"
      style={{ gridColumn: `span ${sectionUnits}` }}
    >
      <div className="flex h-7 items-center px-1.5">
        <span aria-hidden="true" className={`mr-1.5 h-2.5 w-0.5 shrink-0 ${toneClasses[chapter.tone].marker}`} />
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              aria-disabled={!chapter.isNodeAvailable}
              className="min-w-0 flex-1 cursor-pointer truncate text-left text-body-sm font-medium text-ink aria-disabled:cursor-not-allowed aria-disabled:opacity-45"
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
              className="ml-0.5 grid size-5 shrink-0 cursor-pointer place-items-center text-faint transition-colors hover:text-ink"
              onClick={() => onOpenHistory(chapter.nodeId)}
            >
              <History aria-hidden="true" size={11} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="top">查看归档历史</TooltipContent>
        </Tooltip>
      </div>
      <div className="flex h-20 items-stretch gap-px overflow-hidden bg-canvas">
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
          className={`h-20 min-w-1 flex-1 cursor-pointer border bg-film transition-colors focus-visible:relative focus-visible:z-10 focus-visible:outline-none aria-disabled:cursor-not-allowed aria-disabled:opacity-35 ${toneClasses[tone].highlight} ${selected ? toneClasses[tone].selected : 'border-transparent'}`}
          onClick={() => {
            if (section.isNodeAvailable) onFocusSection(chapterNodeId, section.nodeId, !expanded)
          }}
        />
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
