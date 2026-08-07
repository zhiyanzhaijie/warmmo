export type ChapterArchiveProjectionStatus = 'pending' | 'ready'

export interface ChapterArchiveSection {
  archiveId: string
  ordinal: number
  sectionOutlineNodeId: string
  chapterSectionNodeId: string
  chapterSectionVersionId?: string
  nodeRevision: number
  title: string
  summary: string
  content: string
  contentHash: string
}

export interface ChapterArchive {
  id: string
  workId: string
  chapterOutlineNodeId: string
  revision: number
  runId: string
  outlineVersionId?: string
  outlineRevision: number
  outlineTitle: string
  outlineContent: string
  summary: string
  sourceDigest: string
  isCurrent: boolean
  projectionStatus: ChapterArchiveProjectionStatus
  sections: ChapterArchiveSection[]
  createdAt: string
  supersededAt?: string
}

export interface ChapterArchiveVisibilitySection {
  sectionOutlineNodeId: string
  chapterSectionNodeId: string
}

export interface ChapterArchiveVisibility {
  chapterOutlineNodeId: string
  sections: ChapterArchiveVisibilitySection[]
}

export interface ChapterArchiveTimelineSection {
  archiveId: string
  ordinal: number
  sectionOutlineNodeId: string
  chapterSectionNodeId: string
  nodeRevision: number
  title: string
  summary: string
}

export interface ChapterArchiveTimeline {
  id: string
  chapterOutlineNodeId: string
  revision: number
  outlineTitle: string
  summary: string
  projectionStatus: ChapterArchiveProjectionStatus
  sections: ChapterArchiveTimelineSection[]
}
