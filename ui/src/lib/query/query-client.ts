import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { CoreApiError } from '@/lib/api/core-api-error'

export const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error, query) => {
      // Only report background refresh failures; initial failures belong to the feature view.
      if (query.state.data === undefined) return
      toast.error(error instanceof Error ? error.message : '刷新数据失败', {
        id: `query-error:${query.queryHash}`,
      })
    },
  }),
  mutationCache: new MutationCache({
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : '操作失败', {
        id: `mutation-error:${error instanceof Error ? error.message : 'unknown'}`,
      })
    },
  }),
  defaultOptions: {
    queries: {
      gcTime: 30 * 60_000,
      retry: (failureCount, error) => {
        if (error instanceof CoreApiError && error.status >= 400 && error.status < 500) {
          return false
        }

        return failureCount < 1
      },
      refetchOnWindowFocus: true,
      refetchOnReconnect: true,
    },
  },
})
