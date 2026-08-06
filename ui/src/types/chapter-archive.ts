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
