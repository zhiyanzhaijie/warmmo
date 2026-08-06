import { useQuery } from '@tanstack/react-query'

import { coreClient } from '@/lib/api/core-client'
import type { ChapterArchive } from '@/types/chapter-archive'

interface ChapterArchivesResponse {
  archives: ChapterArchive[]
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
}

export function useCurrentChapterArchives(workId: string) {
  return useQuery({
    queryKey: chapterArchiveKeys.current(workId),
    queryFn: async ({ signal }) => {
      const encodedWorkId = encodeURIComponent(workId)
      const response = await coreClient<ChapterArchivesResponse>(
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
