import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import { coreClient } from '@/lib/api/core-client'
import type { EnabledModel, ModelCapability } from '@/types/provider'

interface EnabledModelsResponse {
  models: EnabledModel[]
}

export const modelKeys = {
  all: ['models'] as const,
  enabled: () => [...modelKeys.all, 'enabled'] as const,
}

export function useAvailableModels(capability: ModelCapability) {
  const query = useQuery({
    queryKey: modelKeys.enabled(),
    queryFn: async ({ signal }) => {
      const response = await coreClient<EnabledModelsResponse>('/models/enabled', { signal })
      return response.models
    },
    staleTime: Infinity,
    refetchOnWindowFocus: 'always',
  })

  const models = useMemo(
    () => query.data?.filter((model) => model.capability === capability) ?? [],
    [capability, query.data],
  )

  return { ...query, models }
}

export function useContextAgentAvailability() {
  const query = useAvailableModels('embedding')
  return {
    ...query,
    isAvailable: !query.isPending && !query.isError && query.models.length > 0,
  }
}
