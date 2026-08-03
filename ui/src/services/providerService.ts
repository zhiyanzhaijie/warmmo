import type {
  EnabledModel,
  ProviderConfiguration,
  ProviderDefinition,
  SaveProviderConfiguration,
  TestProviderConfiguration,
  ProviderTestResult,
} from '../types/provider'

interface CatalogResponse {
  providers: ProviderDefinition[]
}

interface ConfigurationsResponse {
  configurations: ProviderConfiguration[]
}

interface EnabledModelsResponse {
  models: EnabledModel[]
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...init?.headers,
    },
  })

  if (!response.ok) {
    const error = (await response.json().catch(() => null)) as { message?: string } | null
    throw new Error(error?.message ?? `Core 返回 HTTP ${response.status}`)
  }
  return response.json() as Promise<T>
}

export async function fetchProviderSettings(signal?: AbortSignal) {
  const [catalog, configurations] = await Promise.all([
    requestJSON<CatalogResponse>('/api/v1/model-catalog', { signal }),
    requestJSON<ConfigurationsResponse>('/api/v1/agent-providers', { signal }),
  ])
  return { providers: catalog.providers, configurations: configurations.configurations }
}

export function saveProviderConfiguration(input: SaveProviderConfiguration) {
  return requestJSON<ProviderConfiguration>(`/api/v1/agent-providers/${encodeURIComponent(input.providerId)}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export function testProviderConfiguration(providerId: string, input: TestProviderConfiguration) {
  return requestJSON<ProviderTestResult>(`/api/v1/agent-providers/${encodeURIComponent(providerId)}/test`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function deleteProviderConfiguration(providerId: string) {
  const response = await fetch(`/api/v1/agent-providers/${encodeURIComponent(providerId)}`, {
    method: 'DELETE',
  })
  if (!response.ok) {
    const error = (await response.json().catch(() => null)) as { message?: string } | null
    throw new Error(error?.message ?? `Core 返回 HTTP ${response.status}`)
  }
}

export async function fetchEnabledModels(signal?: AbortSignal) {
  const response = await requestJSON<EnabledModelsResponse>('/api/v1/models/enabled', { signal })
  return response.models
}
