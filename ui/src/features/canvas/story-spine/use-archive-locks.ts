import { useMemo } from 'react'

import { useCurrentChapterArchives } from '@/apis/chapter-archive-apis'
import { collectArchiveLockedNodeIds } from '@/features/canvas/story-spine/archive-visibility'

export function useArchiveLocks(workId: string, enabled = true) {
  const archivesQuery = useCurrentChapterArchives(workId, enabled)
  const lockedNodeIds = useMemo(() => collectArchiveLockedNodeIds(archivesQuery.data ?? []), [archivesQuery.data])
  return {
    lockedNodeIds,
    isResolved: archivesQuery.isSuccess,
    isError: archivesQuery.isError,
  }
}
