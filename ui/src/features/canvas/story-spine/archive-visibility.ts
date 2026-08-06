import type { ChapterArchive } from '@/types/chapter-archive'

export function collectArchiveLockedNodeIds(archives: ChapterArchive[]) {
  const lockedNodeIds = new Set<string>()
  for (const archive of archives) {
    lockedNodeIds.add(archive.chapterOutlineNodeId)
    for (const section of archive.sections) {
      lockedNodeIds.add(section.sectionOutlineNodeId)
      lockedNodeIds.add(section.chapterSectionNodeId)
    }
  }
  return lockedNodeIds
}

export interface CollapsedArchiveGraph {
  hiddenNodeIds: ReadonlySet<string>
  proxyNodeIds: ReadonlyMap<string, string>
}

export function collectCollapsedArchiveGraph(
  archives: ChapterArchive[],
  expandedChapterNodeIds: ReadonlySet<string>,
): CollapsedArchiveGraph {
  const hiddenNodeIds = new Set<string>()
  const proxyNodeIds = new Map<string, string>()
  for (const archive of archives) {
    if (expandedChapterNodeIds.has(archive.chapterOutlineNodeId)) continue
    for (const section of archive.sections) {
      hiddenNodeIds.add(section.sectionOutlineNodeId)
      hiddenNodeIds.add(section.chapterSectionNodeId)
      proxyNodeIds.set(section.sectionOutlineNodeId, archive.chapterOutlineNodeId)
      proxyNodeIds.set(section.chapterSectionNodeId, archive.chapterOutlineNodeId)
    }
  }
  return { hiddenNodeIds, proxyNodeIds }
}
