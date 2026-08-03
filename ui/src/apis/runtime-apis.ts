import { useQuery } from '@tanstack/react-query'

import { coreClient } from '@/lib/api/core-client'
import type { RuntimeInfo, RuntimeRequestState } from '@/types/runtime'

export function useRuntimeInfo() {
  const query = useQuery({
    queryKey: ['runtime', 'info'],
    queryFn: async ({ signal }) => {
      const startedAt = performance.now()
      const data = await coreClient<RuntimeInfo>('/runtime', { signal })
      return { data, latencyMs: Math.round(performance.now() - startedAt) }
    },
    staleTime: 5_000,
  })

  let state: RuntimeRequestState
  if (query.isPending) {
    state = { status: 'loading' }
  } else if (query.isError) {
    state = { status: 'error', message: query.error instanceof Error ? query.error.message : '未知连接错误' }
  } else {
    state = { status: 'success', data: query.data.data, latencyMs: query.data.latencyMs }
  }

  return { state, isRefreshing: query.isFetching, reload: () => void query.refetch() }
}
