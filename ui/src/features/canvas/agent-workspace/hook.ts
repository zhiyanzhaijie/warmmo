import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'

import { canvasKeys } from '@/apis/canvas-apis'
import { isTerminalAgentEvent, streamedAgentEventTypes } from '@/features/canvas/agent-workspace/events'
import type { AgentEvent } from '@/types/canvas'

export function useAgentRunStream(workId: string) {
  const queryClient = useQueryClient()
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [draft, setDraft] = useState('')
  const [activeRunId, setActiveRunId] = useState<string | null>(null)
  const [streamError, setStreamError] = useState('')
  const eventSourceRef = useRef<EventSource | null>(null)

  useEffect(() => () => eventSourceRef.current?.close(), [])

  const streamRun = useCallback((runId: string) => {
    eventSourceRef.current?.close()
    setActiveRunId(runId)
    setEvents([])
    setDraft('')
    setStreamError('')

    const source = new EventSource(`/api/v1/agent-runs/${runId}/events`)
    eventSourceRef.current = source

    const receive = (message: MessageEvent<string>) => {
      let event: AgentEvent
      try {
        event = JSON.parse(message.data) as AgentEvent
      } catch {
        source.close()
        setActiveRunId(null)
        setStreamError('过程流返回了无法解析的数据')
        return
      }

      const delta = event.data?.delta
      if (event.type === 'message.delta' && typeof delta === 'string') {
        setDraft((current) => current + delta)
      } else {
        setEvents((current) => [...current, event])
      }

      if (isTerminalAgentEvent(event.type)) {
        source.close()
        setActiveRunId(null)
        void queryClient.invalidateQueries({ queryKey: canvasKeys.nodes(workId) })
        void queryClient.invalidateQueries({ queryKey: canvasKeys.candidates(workId) })
      }
    }

    for (const type of streamedAgentEventTypes) source.addEventListener(type, receive as EventListener)
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) return
      source.close()
      setActiveRunId(null)
      setStreamError('过程流连接已断开')
    }
  }, [queryClient, workId])

  return {
    activeRunId,
    draft,
    events,
    streamError,
    streamRun,
  }
}
