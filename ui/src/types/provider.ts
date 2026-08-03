export type ModelCapability = 'text' | 'image'

export interface ModelDefinition {
  id: string
  name: string
  capability: ModelCapability
  description: string
}

export interface ProviderDefinition {
  id: string
  name: string
  defaultBaseUrl: string
  models: ModelDefinition[]
}

export interface ProviderConfiguration {
  id: string
  providerId: string
  baseUrl: string
  modelIds: string[]
  apiKeyConfigured: boolean
  apiKeyHint?: string
  updatedAt: string
}

export interface SaveProviderConfiguration {
  providerId: string
  baseUrl: string
  modelIds: string[]
  apiKey: string
}

export interface TestProviderConfiguration {
  baseUrl: string
  apiKey: string
}

export interface ProviderTestResult {
  valid: boolean
  message: string
  latencyMs: number
}

export interface EnabledModel {
  providerId: string
  providerName: string
  modelId: string
  modelName: string
  capability: ModelCapability
}
