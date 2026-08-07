import { useQuery } from '@tanstack/react-query'

import { coreClient } from '@/lib/api/core-client'
import type { ChapterArchive, ChapterArchiveTimeline, ChapterArchiveVisibility } from '@/types/chapter-archive'

interface ChapterArchivesResponse {
  archives: ChapterArchive[]
}

interface ChapterArchiveVisibilityResponse {
  archives: ChapterArchiveVisibility[]
}

export const chapterArchiveKeys = {
  all: ['chapter-archives'] as const,
  work: (workId: string) => [...chapterArchiveKeys.all, workId] as const,
  current: (workId: string) => [...chapterArchiveKeys.work(workId), 'current'] as const,
  history: (workId: string, chapterOutlineNodeId: string) => [
    ...chapterArchiveKeys.work(workId),
    'history',
    chapterOutlineNodeId,
  ] as const,
  storySpine: (workId: string, page: number, pageSize: number) => [
    ...chapterArchiveKeys.work(workId),
    'story-spine',
    page,
    pageSize,
  ] as const,
}

export function useCurrentChapterArchives(workId: string) {
  return useQuery({
    queryKey: chapterArchiveKeys.current(workId),
    queryFn: async ({ signal }) => {
      const encodedWorkId = encodeURIComponent(workId)
      const response = await coreClient<ChapterArchiveVisibilityResponse>(
        `/works/${encodedWorkId}/chapter-archives`,
        { signal },
      )
      return response.archives
    },
  })
}

export function useChapterArchiveHistory(workId: string, chapterOutlineNodeId: string | null) {
  return useQuery({
    queryKey: chapterArchiveKeys.history(workId, chapterOutlineNodeId ?? ''),
    enabled: chapterOutlineNodeId !== null,
    queryFn: async ({ signal }) => {
      const encodedWorkId = encodeURIComponent(workId)
      const encodedNodeId = encodeURIComponent(chapterOutlineNodeId ?? '')
      const response = await coreClient<ChapterArchivesResponse>(
        `/works/${encodedWorkId}/nodes/${encodedNodeId}/chapter-archives`,
        { signal },
      )
      return response.archives
    },
  })
}

export interface PaginationMetadata {
  page: number
  pageSize: number
  total: number
  totalPages: number
  hasPrevious: boolean
  hasNext: boolean
}

export function useStorySpine(workId: string, page = 1, pageSize = 20) {
  return useQuery({
    queryKey: chapterArchiveKeys.storySpine(workId, page, pageSize),
    queryFn: async ({ signal }) => {
      const encodedWorkId = encodeURIComponent(workId)
      return coreClient<{ items: ChapterArchiveTimeline[]; pagination: PaginationMetadata }>(
        `/works/${encodedWorkId}/story-spine?page=${page}&pageSize=${pageSize}`,
        { signal },
      )
    },
  })
}
