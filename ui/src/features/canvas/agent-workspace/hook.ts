import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'

import { canvasKeys } from '@/apis/canvas-apis'
import { isTerminalAgentEvent, streamedAgentEventTypes } from '@/features/canvas/agent-workspace/events'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { AgentEvent } from '@/types/canvas'

export function useAgentRunStream(workId: string) {
  const queryClient = useQueryClient()
  const [activeRunId, setActiveRunId] = useState<string | null>(null)
  const [streamError, setStreamError] = useState('')
  const attachNodeAgentRun = useFlowNodeStore((state) => state.actions.attachNodeAgentRun)
  const appendNodeAgentEvent = useFlowNodeStore((state) => state.actions.appendNodeAgentEvent)
  const failNodeAgentRun = useFlowNodeStore((state) => state.actions.failNodeAgentRun)
  const eventSourceRef = useRef<EventSource | null>(null)

  useEffect(() => () => eventSourceRef.current?.close(), [])

  const streamRun = useCallback((runId: string, nodeId: string, afterSequence = 0) => {
    eventSourceRef.current?.close()
    setActiveRunId(runId)
    setStreamError('')
    attachNodeAgentRun(nodeId, runId)

    const source = new EventSource(`/api/v1/agent-runs/${runId}/events?afterSequence=${afterSequence}`)
    eventSourceRef.current = source

    const receive = (message: MessageEvent<string>) => {
      let event: AgentEvent
      try {
        event = JSON.parse(message.data) as AgentEvent
      } catch {
        source.close()
        setActiveRunId(null)
        const message = '过程流返回了无法解析的数据'
        setStreamError(message)
        failNodeAgentRun(nodeId, message)
        return
      }

      appendNodeAgentEvent(nodeId, event)

      if (event.type === 'approval.required') {
        source.close()
        setActiveRunId(null)
        return
      }

      if (isTerminalAgentEvent(event.type)) {
        source.close()
        setActiveRunId(null)
        if (event.type === 'run.failed') {
          const message = event.data?.message
          if (typeof message === 'string') setStreamError(message)
        }
        void queryClient.invalidateQueries({ queryKey: canvasKeys.nodes(workId) })
        void queryClient.invalidateQueries({ queryKey: canvasKeys.candidates(workId) })
      }
    }

    for (const type of streamedAgentEventTypes) source.addEventListener(type, receive as EventListener)
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) return
      source.close()
      setActiveRunId(null)
      const message = '过程流连接已断开'
      setStreamError(message)
      failNodeAgentRun(nodeId, message)
    }
  }, [appendNodeAgentEvent, attachNodeAgentRun, failNodeAgentRun, queryClient, workId])

  return {
    activeRunId,
    streamError,
    streamRun,
  }
}
