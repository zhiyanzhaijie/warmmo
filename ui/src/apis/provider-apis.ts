import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { coreClient } from '@/lib/api/core-client'
import type {
  ProviderConfiguration,
  ProviderDefinition,
  ProviderTestResult,
  SaveProviderConfiguration,
  TestProviderConfiguration,
} from '@/types/provider'

import { modelKeys } from './model-apis'

interface CatalogResponse {
  providers: ProviderDefinition[]
}

interface ConfigurationsResponse {
  configurations: ProviderConfiguration[]
}

interface TestProviderVariables {
  providerId: string
  input: TestProviderConfiguration
}

const providerKeys = {
  all: ['providers'] as const,
  catalog: () => [...providerKeys.all, 'catalog'] as const,
  configurations: () => [...providerKeys.all, 'configurations'] as const,
}

export function useModelCatalog() {
  return useQuery({
    queryKey: providerKeys.catalog(),
    queryFn: ({ signal }) => coreClient<CatalogResponse>('/model-catalog', { signal }),
    staleTime: Infinity,
    refetchOnWindowFocus: 'always',
  })
}

export function useProviderConfigurations() {
  return useQuery({
    queryKey: providerKeys.configurations(),
    queryFn: ({ signal }) => coreClient<ConfigurationsResponse>('/agent-providers', { signal }),
    staleTime: Infinity,
    refetchOnWindowFocus: 'always',
  })
}

export function useSaveProvider() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (input: SaveProviderConfiguration) =>
      coreClient<ProviderConfiguration>(`/agent-providers/${encodeURIComponent(input.providerId)}`, {
        method: 'PUT',
        body: input,
      }),
    onSuccess: () => invalidateProviderModels(queryClient),
  })
}

export function useDeleteProvider() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (providerId: string) =>
      coreClient(`/agent-providers/${encodeURIComponent(providerId)}`, { method: 'DELETE' }),
    onSuccess: () => invalidateProviderModels(queryClient),
  })
}

export function useTestProvider() {
  return useMutation({
    mutationFn: ({ providerId, input }: TestProviderVariables) =>
      coreClient<ProviderTestResult>(`/agent-providers/${encodeURIComponent(providerId)}/test`, {
        method: 'POST',
        body: input,
      }),
  })
}

function invalidateProviderModels(queryClient: ReturnType<typeof useQueryClient>) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: providerKeys.configurations() }),
    queryClient.invalidateQueries({ queryKey: modelKeys.enabled() }),
  ])
}
