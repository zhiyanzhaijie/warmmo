import type { RuntimeInfo } from '../types/runtime'

export async function fetchRuntimeInfo(signal?: AbortSignal): Promise<RuntimeInfo> {
  const response = await fetch('/api/v1/runtime', {
    headers: { Accept: 'application/json' },
    signal,
  })

  if (!response.ok) {
    throw new Error(`Core 返回 HTTP ${response.status}`)
  }

  return response.json() as Promise<RuntimeInfo>
}
