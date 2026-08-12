import { EventStreamContentType, fetchEventSource, type EventSourceMessage } from '@microsoft/fetch-event-source'

import { coreApiURL } from './core-config'
import { authenticatedCoreFetch } from './core-fetch'

interface CoreEventStreamOptions {
  onError: (error: unknown) => void
  onMessage: (message: EventSourceMessage) => void
}

export interface CoreEventStream {
  close: () => void
}

export function openCoreEventStream(path: string, options: CoreEventStreamOptions): CoreEventStream {
  const controller = new AbortController()
  void fetchEventSource(coreApiURL(path), {
    fetch: authenticatedCoreFetch,
    openWhenHidden: true,
    signal: controller.signal,
    async onopen(response) {
      if (!response.ok) throw new Error(`Core 事件流返回 HTTP ${response.status}`)
      if (!response.headers.get('Content-Type')?.startsWith(EventStreamContentType)) {
        throw new Error('Core 事件流响应格式无效')
      }
    },
    onmessage: options.onMessage,
    onclose() {
      throw new Error('Core 事件流意外关闭')
    },
    onerror(error) {
      throw error
    },
  }).catch((error: unknown) => {
    if (!controller.signal.aborted) options.onError(error)
  })

  return { close: () => controller.abort() }
}
