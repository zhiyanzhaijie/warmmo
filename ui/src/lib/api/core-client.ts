import { ofetch } from 'ofetch'

import { CoreApiError } from './core-api-error'
import { coreApiBaseURL } from './core-config'
import { authenticatedCoreFetch } from './core-fetch'

interface CoreErrorPayload {
  message?: string
  code?: string
}

export const coreClient = ofetch.create({
  baseURL: coreApiBaseURL,
  retry: 0,
  timeout: 10_000,

  onRequest({ options }) {
    options.headers.set('Accept', 'application/json')
  },

  onRequestError({ error }) {
    if (isAbortError(error)) throw error
    throw new CoreApiError('无法连接本地 Warmmo Core', 0, 'CORE_UNREACHABLE', { cause: error })
  },

  onResponse({ response, options }) {
    if (!response.ok || response.status === 204 || (options.responseType !== undefined && options.responseType !== 'json')) return
    if (!response.headers.get('Content-Type')?.includes('application/json')) {
      throw new CoreApiError('Core 返回了无法识别的响应格式', response.status, 'INVALID_CORE_RESPONSE')
    }
  },

  onResponseError({ response }) {
    const payload = response._data as CoreErrorPayload | undefined
    throw new CoreApiError(
      payload?.message ?? `Core 返回 HTTP ${response.status}`,
      response.status,
      payload?.code,
    )
  },
}, { fetch: authenticatedCoreFetch })

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError'
}
