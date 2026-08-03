import { QueryClient } from '@tanstack/react-query'

import { CoreApiError } from '@/lib/api/core-api-error'

export const queryClient = new QueryClient({
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
