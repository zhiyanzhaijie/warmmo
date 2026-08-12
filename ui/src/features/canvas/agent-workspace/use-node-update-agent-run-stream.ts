import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef } from 'react'
import { toast } from 'sonner'

import { canvasKeys } from '@/apis/canvas-apis'
import { isTerminalAgentEvent, streamedAgentEventTypes } from '@/features/canvas/agent-workspace/events'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { openCoreEventStream, type CoreEventStream } from '@/lib/api/core-event-stream'
import type { AgentEvent } from '@/types/canvas'

const streamedAgentEventTypeSet = new Set(streamedAgentEventTypes)

export function useNodeUpdateAgentRunStream(workId: string) {
  const queryClient = useQueryClient()
  const attachNodeAgentRun = useFlowNodeStore((state) => state.actions.attachNodeAgentRun)
  const appendNodeAgentEvent = useFlowNodeStore((state) => state.actions.appendNodeAgentEvent)
  const dismissNodeAgentRun = useFlowNodeStore((state) => state.actions.dismissNodeAgentRun)
  const eventSourcesRef = useRef(new Map<string, CoreEventStream>())

  useEffect(() => () => {
    for (const source of eventSourcesRef.current.values()) source.close()
    eventSourcesRef.current.clear()
  }, [])

  const streamRun = useCallback((runId: string, nodeId: string, afterSequence = 0) => {
    eventSourcesRef.current.get(nodeId)?.close()
    attachNodeAgentRun(nodeId, runId)

    let source: CoreEventStream
    const closeSource = () => {
      source.close()
      if (eventSourcesRef.current.get(nodeId) === source) eventSourcesRef.current.delete(nodeId)
    }

    const receive = (data: string) => {
      let event: AgentEvent
      try {
        event = JSON.parse(data) as AgentEvent
      } catch {
        closeSource()
        toast.error('过程流返回了无法解析的数据')
        dismissNodeAgentRun(nodeId)
        return
      }

      if (event.type === 'run.failed') {
        const error = typeof event.data?.message === 'string' ? event.data.message : 'Agent 执行失败'
        toast.error(error)
      }
      appendNodeAgentEvent(nodeId, event)

      if (event.type === 'approval.required') {
        closeSource()
        return
      }
      if (!isTerminalAgentEvent(event.type)) return

      closeSource()
      if (event.type === 'run.completed') {
        void queryClient.invalidateQueries({ queryKey: canvasKeys.nodes(workId) })
      }
    }

    source = openCoreEventStream(`/agent-runs/${runId}/events?afterSequence=${afterSequence}`, {
      onMessage: (message) => {
        if (streamedAgentEventTypeSet.has(message.event)) receive(message.data)
      },
      onError: () => {
        closeSource()
        toast.error('过程流连接已断开')
        dismissNodeAgentRun(nodeId)
      },
    })
    eventSourcesRef.current.set(nodeId, source)
  }, [appendNodeAgentEvent, attachNodeAgentRun, dismissNodeAgentRun, queryClient, workId])

  return { streamRun }
}
