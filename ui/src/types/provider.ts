export type ModelCapability = 'text' | 'image' | 'embedding'

export const CANONICAL_EMBEDDING_PROVIDER_ID = 'siliconflow'
export const CANONICAL_EMBEDDING_MODEL_ID = 'Qwen/Qwen3-Embedding-0.6B'
export const CANONICAL_EMBEDDING_DIMENSIONS = 1024

export interface ModelDefinition {
  id: string
  name: string
  capability: ModelCapability
  description: string
  dimensions?: number
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
  modelId?: string
}

export interface ProviderTestResult {
  valid: boolean
  message: string
  latencyMs: number
}

export interface ModelReference {
  providerId: string
  modelId: string
}

export interface EnabledModel extends ModelReference {
  providerName: string
  modelName: string
  capability: ModelCapability
}
