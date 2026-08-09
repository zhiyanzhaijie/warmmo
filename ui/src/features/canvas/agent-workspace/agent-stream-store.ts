import { create } from 'zustand'

interface AgentStreamState {
  messages: Record<string, string>
}

const useAgentStreamStore = create<AgentStreamState>(() => ({ messages: {} }))
const pendingDeltas = new Map<string, string>()
let scheduledFrame: number | null = null

export function appendAgentStreamDelta(messageId: string, delta: string) {
  if (delta === '') return
  pendingDeltas.set(messageId, (pendingDeltas.get(messageId) ?? '') + delta)
  if (scheduledFrame !== null) return
  scheduledFrame = requestAnimationFrame(flushPendingAgentStreams)
}

export function flushAgentStream(messageId: string) {
  const delta = pendingDeltas.get(messageId)
  if (delta === undefined) return
  pendingDeltas.delete(messageId)
  publishDeltas([[messageId, delta]])
}

export function getAgentStreamText(messageId: string) {
  return useAgentStreamStore.getState().messages[messageId] ?? ''
}

export function clearAgentStreams(messageIds: Iterable<string>) {
  const ids = new Set(messageIds)
  if (ids.size === 0) return
  for (const id of ids) pendingDeltas.delete(id)
  useAgentStreamStore.setState((state) => {
    const messages = { ...state.messages }
    for (const id of ids) delete messages[id]
    return { messages }
  })
}

export function useAgentStreamText(messageId: string) {
  return useAgentStreamStore((state) => state.messages[messageId] ?? '')
}

function flushPendingAgentStreams() {
  scheduledFrame = null
  if (pendingDeltas.size === 0) return
  const deltas = [...pendingDeltas.entries()]
  pendingDeltas.clear()
  publishDeltas(deltas)
}

function publishDeltas(deltas: Array<[string, string]>) {
  useAgentStreamStore.setState((state) => {
    const messages = { ...state.messages }
    for (const [messageId, delta] of deltas) {
      messages[messageId] = (messages[messageId] ?? '') + delta
    }
    return { messages }
  })
}
