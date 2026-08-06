import type { StorySpineChapter, StorySpineTone } from '@/features/canvas/story-spine/types'
import type { CanvasNode } from '@/types/canvas'
import type { ChapterArchive } from '@/types/chapter-archive'

const storySpineTones: StorySpineTone[] = ['cyan', 'magenta', 'amber', 'violet', 'green']

export function toStorySpineChapters(
  archives: ChapterArchive[],
  canvasNodes: CanvasNode[],
): StorySpineChapter[] {
  const availableNodeIds = new Set(canvasNodes.map((node) => node.id))

  return archives.map((archive, index) => ({
    archiveId: archive.id,
    archiveRevision: archive.revision,
    title: archive.outlineTitle,
    summary: archive.summary,
    nodeId: archive.chapterOutlineNodeId,
    isNodeAvailable: availableNodeIds.has(archive.chapterOutlineNodeId),
    isProjectionPending: archive.projectionStatus === 'pending',
    tone: storySpineTones[index % storySpineTones.length],
    sections: archive.sections.map((section) => ({
      id: `${archive.id}:${section.chapterSectionNodeId}`,
      ordinal: section.ordinal,
      title: section.title,
      summary: section.summary,
      nodeId: section.chapterSectionNodeId,
      nodeRevision: section.nodeRevision,
      isNodeAvailable: availableNodeIds.has(section.chapterSectionNodeId),
    })),
  }))
}
