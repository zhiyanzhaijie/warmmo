import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { toast } from 'sonner'
import { useQueryClient } from '@tanstack/react-query'

import {
  canvasKeys,
  type CollaborativeAgentTarget,
  useCreateCollaborativeAgentRun,
  useRespondToAgentRun,
} from '@/apis/canvas-apis'
import {
  appendAgentStreamDelta,
  clearAgentStreams,
  flushAgentStream,
  getAgentStreamText,
} from '@/features/canvas/agent-workspace/agent-stream-store'
import { isTerminalAgentEvent, streamedAgentEventTypes } from '@/features/canvas/agent-workspace/events'
import type { AgentEvent } from '@/types/canvas'
import type { EnabledModel } from '@/types/provider'

export type CollaborativeTurnStatus = 'submitting' | 'running' | 'waiting_input' | 'completed' | 'failed'

export interface CollaborativeTurn {
  clientId: string
  events: AgentEvent[]
  prompt: string
  runId: string | null
  status: CollaborativeTurnStatus
  target: CollaborativeAgentTarget
  error?: string
}

export interface CollaborativePendingInput {
  approvalEventId: string
  clientId: string
  lastSequence: number
  options: string[]
  question: string
  runId: string
}

export function useCollaborativeAgentSession(workId: string) {
  const queryClient = useQueryClient()
  const createRun = useCreateCollaborativeAgentRun(workId)
  const respondToRun = useRespondToAgentRun()
  const [turns, setTurns] = useState<CollaborativeTurn[]>([])
  const eventSourcesRef = useRef(new Map<string, EventSource>())
  const streamMessageIdsRef = useRef(new Set<string>())
  const activeTurn = turns.findLast((turn) =>
    turn.status === 'submitting' || turn.status === 'running' || turn.status === 'waiting_input')
  const pendingInput = useMemo(() => getPendingInput(activeTurn), [activeTurn])

  useEffect(() => () => {
    for (const source of eventSourcesRef.current.values()) source.close()
    eventSourcesRef.current.clear()
    clearAgentStreams(streamMessageIdsRef.current)
    streamMessageIdsRef.current.clear()
  }, [])

  const streamRun = useCallback((clientId: string, runId: string, afterSequence = 0, follow = false) => {
    eventSourcesRef.current.get(runId)?.close()
    const source = new EventSource(
      `/api/v1/agent-runs/${runId}/events?afterSequence=${afterSequence}${follow ? '&follow=true' : ''}`,
    )
    eventSourcesRef.current.set(runId, source)
    const closeSource = () => {
      source.close()
      if (eventSourcesRef.current.get(runId) === source) eventSourcesRef.current.delete(runId)
    }
    const receive = (message: MessageEvent<string>) => {
      let event: AgentEvent
      try {
        event = JSON.parse(message.data) as AgentEvent
      } catch {
        closeSource()
        updateTurn(setTurns, clientId, (turn) => ({
          ...turn,
          error: '过程流返回了无法解析的数据',
          status: 'failed',
        }))
        return
      }

      if (event.type === 'message.delta') {
        const delta = eventString(event, 'delta')
        if (delta !== undefined) appendAgentStreamDelta(clientId, delta)
        return
      }
      if (event.type === 'approval.required' || isTerminalAgentEvent(event.type)) flushAgentStream(clientId)
      updateTurn(setTurns, clientId, (turn) => appendEvent(turn, event))
      if (event.type === 'run.failed') toast.error(eventString(event, 'message') ?? 'Agent 执行失败')
      if (event.type === 'candidate.created') {
        void queryClient.invalidateQueries({ queryKey: canvasKeys.candidates(workId) })
      }
      if (event.type === 'approval.required') {
        closeSource()
        return
      }
      if (!isTerminalAgentEvent(event.type)) return

      closeSource()
      if (event.type === 'run.completed') {
        void queryClient.invalidateQueries({ queryKey: canvasKeys.candidates(workId) })
      }
    }

    for (const type of streamedAgentEventTypes) source.addEventListener(type, receive as EventListener)
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) return
      closeSource()
      flushAgentStream(clientId)
      updateTurn(setTurns, clientId, (turn) => ({
        ...turn,
        error: '过程流连接已断开',
        status: 'failed',
      }))
      toast.error('过程流连接已断开')
    }
  }, [queryClient, workId])

  // Candidate accept/reject happens outside the drawer's SSE connection. Keep
  // a continuation stream open for completed turns so decisions and the next
  // generation re-enter the same chat timeline.
  useEffect(() => {
    const resumableTurns = turns.filter((turn) =>
      turn.runId !== null && turn.status === 'completed' &&
      turn.events.some((event) => event.type === 'candidate.created'))
    for (const turn of resumableTurns) {
      if (turn.runId === null || eventSourcesRef.current.has(turn.runId)) continue
      const lastSequence = turn.events.reduce((latest, event) => Math.max(latest, event.sequence), 0)
      streamRun(turn.clientId, turn.runId, lastSequence, true)
    }
  }, [streamRun, turns])

  const run = useCallback((input: {
    contextNodeIds: string[]
    model: EnabledModel
    prompt: string
    target: CollaborativeAgentTarget
  }) => {
    if (activeTurn !== undefined || !createRun.contextAgentAvailable) return
    const clientId = crypto.randomUUID()
    streamMessageIdsRef.current.add(clientId)
    const prompt = input.prompt.trim()
    const apiPrompt = buildConversationPrompt(turns, prompt)
    setTurns((current) => [...current, {
      clientId,
      events: [],
      prompt,
      runId: null,
      status: 'submitting',
      target: input.target,
    }])
    createRun.mutate({ ...input, prompt: apiPrompt }, {
      onSuccess: (createdRun) => {
        updateTurn(setTurns, clientId, (turn) => ({ ...turn, runId: createdRun.id, status: 'running' }))
        streamRun(clientId, createdRun.id)
      },
      onError: (error) => {
        const message = error instanceof Error ? error.message : '无法创建 Agent Run'
        updateTurn(setTurns, clientId, (turn) => ({ ...turn, error: message, status: 'failed' }))
        toast.error(message)
      },
    })
  }, [activeTurn, createRun, streamRun, turns])

  const respond = useCallback((answer: string) => {
    if (pendingInput === null || answer.trim() === '' || respondToRun.isPending) return
    respondToRun.mutate({
      answer: answer.trim(),
      approvalEventId: pendingInput.approvalEventId,
      runId: pendingInput.runId,
    }, {
      onSuccess: () => {
        updateTurn(setTurns, pendingInput.clientId, (turn) => ({ ...turn, status: 'running' }))
        streamRun(pendingInput.clientId, pendingInput.runId, pendingInput.lastSequence)
      },
      onError: (error) => toast.error(error instanceof Error ? error.message : '提交回答失败'),
    })
  }, [pendingInput, respondToRun, streamRun])

  const clear = useCallback(() => {
    if (activeTurn !== undefined) return
    for (const source of eventSourcesRef.current.values()) source.close()
    eventSourcesRef.current.clear()
    clearAgentStreams(turns.map((turn) => turn.clientId))
    streamMessageIdsRef.current.clear()
    setTurns([])
  }, [activeTurn, turns])

  return {
    activeTurn,
    canUseContextAgent: createRun.contextAgentAvailable,
    clear,
    contextAgentPending: createRun.contextAgentPending,
    pendingInput,
    responding: respondToRun.isPending,
    respond,
    run,
    turns,
  }
}

function appendEvent(turn: CollaborativeTurn, event: AgentEvent): CollaborativeTurn {
  if (turn.events.some((existing) => existing.id === event.id || existing.sequence === event.sequence)) return turn
  const status = event.type === 'approval.required'
    ? 'waiting_input'
    : event.type === 'run.completed'
      ? 'completed'
      : event.type === 'run.failed' || event.type === 'run.cancelled'
        ? 'failed'
        : 'running'
  const error = event.type === 'run.failed'
    ? eventString(event, 'message') ?? 'Agent 执行失败'
    : turn.error
  return { ...turn, error, events: [...turn.events, event], status }
}

function getPendingInput(turn: CollaborativeTurn | undefined): CollaborativePendingInput | null {
  if (turn?.status !== 'waiting_input' || turn.runId === null) return null
  const answeredApprovalIds = new Set(turn.events.flatMap((event) => {
    const approvalEventId = event.type === 'user.response.received' ? event.data?.approvalEventId : undefined
    return typeof approvalEventId === 'string' ? [approvalEventId] : []
  }))
  const event = turn.events.findLast((candidate) =>
    candidate.type === 'approval.required' && !answeredApprovalIds.has(candidate.id))
  if (event === undefined) return null
  return {
    approvalEventId: event.id,
    clientId: turn.clientId,
    lastSequence: turn.events.reduce((latest, current) => Math.max(latest, current.sequence), 0),
    options: Array.isArray(event.data?.options)
      ? event.data.options.filter((option): option is string => typeof option === 'string')
      : [],
    question: eventString(event, 'question') ?? 'Agent 需要你补充信息',
    runId: turn.runId,
  }
}

function updateTurn(
  setTurns: Dispatch<SetStateAction<CollaborativeTurn[]>>,
  clientId: string,
  update: (turn: CollaborativeTurn) => CollaborativeTurn,
) {
  setTurns((current) => current.map((turn) => turn.clientId === clientId ? update(turn) : turn))
}

function buildConversationPrompt(turns: CollaborativeTurn[], request: string) {
  const history = turns.slice(-4).flatMap((turn) => {
    const response = getAgentStreamText(turn.clientId)
    return response === '' ? [] : [`用户：${turn.prompt}\nAgent：${response}`]
  }).join('\n\n').slice(-12_000)
  if (history === '') return request
  return `以下是当前协作会话的最近上下文：\n\n${history}\n\n用户当前请求：${request}`
}

function eventString(event: AgentEvent | undefined, key: string) {
  const value = event?.data?.[key]
  return typeof value === 'string' ? value : undefined
}
