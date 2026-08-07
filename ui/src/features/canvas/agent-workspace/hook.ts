import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef } from 'react'

import { canvasKeys } from '@/apis/canvas-apis'
import { chapterArchiveKeys } from '@/apis/chapter-archive-apis'
import { isTerminalAgentEvent, streamedAgentEventTypes } from '@/features/canvas/agent-workspace/events'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { AgentEvent } from '@/types/canvas'

export function useAgentRunStream(workId: string) {
  const queryClient = useQueryClient()
  const attachNodeAgentRun = useFlowNodeStore((state) => state.actions.attachNodeAgentRun)
  const appendNodeAgentEvent = useFlowNodeStore((state) => state.actions.appendNodeAgentEvent)
  const failNodeAgentRun = useFlowNodeStore((state) => state.actions.failNodeAgentRun)
  const eventSourcesRef = useRef(new Map<string, EventSource>())

  useEffect(() => () => {
    for (const source of eventSourcesRef.current.values()) source.close()
    eventSourcesRef.current.clear()
  }, [])

  const streamRun = useCallback((runId: string, nodeId: string, afterSequence = 0) => {
    eventSourcesRef.current.get(nodeId)?.close()
    attachNodeAgentRun(nodeId, runId)

    const source = new EventSource(`/api/v1/agent-runs/${runId}/events?afterSequence=${afterSequence}`)
    eventSourcesRef.current.set(nodeId, source)
    const closeSource = () => {
      source.close()
      if (eventSourcesRef.current.get(nodeId) === source) eventSourcesRef.current.delete(nodeId)
    }

    const receive = (message: MessageEvent<string>) => {
      let event: AgentEvent
      try {
        event = JSON.parse(message.data) as AgentEvent
      } catch {
        closeSource()
        const message = '过程流返回了无法解析的数据'
        failNodeAgentRun(nodeId, message)
        return
      }

      appendNodeAgentEvent(nodeId, event)

      if (event.type === 'approval.required') {
        closeSource()
        return
      }

      if (isTerminalAgentEvent(event.type)) {
        closeSource()
        const invalidations = [
          queryClient.invalidateQueries({ queryKey: canvasKeys.nodes(workId) }),
          queryClient.invalidateQueries({ queryKey: canvasKeys.edges(workId) }),
          queryClient.invalidateQueries({ queryKey: canvasKeys.candidates(workId) }),
        ]
        if (event.type === 'run.completed' && typeof event.data?.archiveId === 'string') {
          invalidations.push(
            queryClient.invalidateQueries({ queryKey: chapterArchiveKeys.work(workId) }),
            queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) }),
          )
        }
        void Promise.all(invalidations)
      }
    }

    for (const type of streamedAgentEventTypes) source.addEventListener(type, receive as EventListener)
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) return
      closeSource()
      const message = '过程流连接已断开'
      failNodeAgentRun(nodeId, message)
    }
  }, [appendNodeAgentEvent, attachNodeAgentRun, failNodeAgentRun, queryClient, workId])

  return { streamRun }
}
