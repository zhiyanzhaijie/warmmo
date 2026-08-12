import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef } from 'react'
import { toast } from 'sonner'

import { canvasKeys, type NodeDerivationTarget } from '@/apis/canvas-apis'
import { chapterArchiveKeys } from '@/apis/chapter-archive-apis'
import { isTerminalAgentEvent, streamedAgentEventTypes } from '@/features/canvas/agent-workspace/events'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import { openCoreEventStream, type CoreEventStream } from '@/lib/api/core-event-stream'
import type { AgentEvent } from '@/types/canvas'

const streamedAgentEventTypeSet = new Set(streamedAgentEventTypes)

export function useNodeDerivationAgentRunStream(workId: string) {
  const queryClient = useQueryClient()
  const attachNodeAgentRun = useFlowNodeStore((state) => state.actions.attachNodeAgentRun)
  const appendNodeAgentEvent = useFlowNodeStore((state) => state.actions.appendNodeAgentEvent)
  const dismissNodeAgentRun = useFlowNodeStore((state) => state.actions.dismissNodeAgentRun)
  const eventSourcesRef = useRef(new Map<string, CoreEventStream>())

  useEffect(() => () => {
    for (const source of eventSourcesRef.current.values()) source.close()
    eventSourcesRef.current.clear()
  }, [])

  const streamRun = useCallback((runId: string, nodeId: string, target: NodeDerivationTarget) => {
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
        const message = '过程流返回了无法解析的数据'
        toast.error(message)
        dismissNodeAgentRun(nodeId)
        return
      }

      if (event.type === 'run.failed') {
        toast.error(agentRunErrorMessage(event))
      }
      appendNodeAgentEvent(nodeId, event)

      if (event.type === 'approval.required') {
        closeSource()
        return
      }

      if (isTerminalAgentEvent(event.type)) {
        closeSource()
        if (event.type !== 'run.completed') return
        if (target === 'chapter-archive') {
          const invalidations = [
            queryClient.invalidateQueries({ queryKey: canvasKeys.nodes(workId) }),
            queryClient.invalidateQueries({ queryKey: chapterArchiveKeys.work(workId) }),
            queryClient.invalidateQueries({ queryKey: canvasKeys.history(workId) }),
          ]
          if (Array.isArray(event.data?.candidateIds) && event.data.candidateIds.length > 0) {
            invalidations.push(queryClient.invalidateQueries({ queryKey: canvasKeys.candidates(workId) }))
          }
          void Promise.all(invalidations)
          return
        }
        void Promise.all([
          queryClient.invalidateQueries({ queryKey: canvasKeys.nodes(workId) }),
          queryClient.invalidateQueries({ queryKey: canvasKeys.edges(workId) }),
        ])
      }
    }

    source = openCoreEventStream(`/agent-runs/${runId}/events`, {
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

function agentRunErrorMessage(event: AgentEvent) {
  const rawMessage = typeof event.data?.message === 'string' ? event.data.message : 'Agent 执行失败'
  const incompleteArchive = rawMessage.match(/chapter archive is incomplete: all (\d+) section outlines must have completed chapter sections/i)
  if (incompleteArchive !== null) {
    return `章节归档失败：共规划了 ${incompleteArchive[1]} 个章节小节，请完成全部小节正文后再归档。`
  }
  return rawMessage
}
