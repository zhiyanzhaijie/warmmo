import type { LucideIcon } from 'lucide-react'
import { BrainCircuit, Check, Compass, Feather, LoaderCircle, Search, Wrench } from 'lucide-react'
import { memo, useMemo } from 'react'

import {
  ChainOfThought,
  ChainOfThoughtContent,
  ChainOfThoughtHeader,
  ChainOfThoughtStep,
} from '@/components/ai-elements/chain-of-thought'
import type { CollaborativeTurn } from '@/features/canvas/agent-workspace/use-collaborative-agent-session'
import type { AgentEvent } from '@/types/canvas'

interface ProgressStep {
  description?: string
  icon: LucideIcon
  id: string
  label: string
  status: 'active' | 'complete'
}

export const CollaborativeAgentProgress = memo(function CollaborativeAgentProgress({
  turn,
}: {
  turn: CollaborativeTurn
}) {
  const steps = useMemo(() => buildSteps(turn), [turn])
  const running = turn.status === 'submitting' || turn.status === 'running'
  const summary = turn.status === 'waiting_input'
    ? '等待你的回答'
    : turn.status === 'completed'
      ? '协作过程'
      : turn.status === 'failed'
        ? '协作中断'
        : steps.findLast((step) => step.status === 'active')?.label ?? '正在理解意图'

  return (
    <ChainOfThought defaultOpen={turn.status !== 'completed'} className="space-y-0">
      <ChainOfThoughtHeader className="min-h-7 gap-space-xs text-body-sm text-mute [&>svg:first-child]:hidden [&>svg:last-child]:size-3">
        <span className="flex min-w-0 items-center gap-space-xs">
          {running ? <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" /> : <BrainCircuit aria-hidden="true" className="size-3.5" />}
          <span className="truncate">{summary}</span>
        </span>
      </ChainOfThoughtHeader>
      <ChainOfThoughtContent className="mt-space-xs border-l border-hairline pl-space-sm">
        {steps.map((step) => (
          <ChainOfThoughtStep
            key={step.id}
            className="gap-space-xs text-body-sm [&>div:first-child>div]:hidden [&>div:last-child]:space-y-0"
            description={step.description}
            icon={step.icon}
            label={step.label}
            status={step.status}
          />
        ))}
      </ChainOfThoughtContent>
    </ChainOfThought>
  )
})

function buildSteps(turn: CollaborativeTurn) {
  if (turn.events.length === 0) {
    return [{ id: 'submit', icon: BrainCircuit, label: '提交协作意图', status: 'active' as const }]
  }
  const steps: ProgressStep[] = []
  const contextEvent = lastEvent(turn.events, 'context.ready') ?? lastEvent(turn.events, 'context.preparing')
  if (contextEvent !== undefined) {
    const count = eventNumber(contextEvent, 'nodeCount')
    steps.push({
      id: 'context',
      icon: Search,
      label: '检索画布上下文',
      description: count === undefined ? undefined : `${count} 个节点`,
      status: hasEvent(turn.events, 'context.ready') ? 'complete' : 'active',
    })
  }

  const roleEvents = turn.events.filter((event) => event.type === 'role.started')
  for (const event of roleEvents) {
    const role = eventString(event, 'role')
    const completed = turn.events.some((candidate) =>
      candidate.type === 'role.completed' && eventString(candidate, 'role') === role)
    steps.push({
      id: `role:${event.sequence}`,
      icon: role === 'creator' ? Feather : role === 'writer' ? Check : Compass,
      label: role === 'creator' ? 'Creator 生成创作结果' : role === 'writer' ? 'Writer 统一文风' : 'Planner 梳理意图与计划',
      description: eventString(event, 'skillId'),
      status: completed ? 'complete' : 'active',
    })
  }

  const toolEvents = turn.events.filter((event) => event.type === 'tool.requested')
  for (const event of toolEvents) {
    const name = eventString(event, 'name') ?? 'Agent Tool'
    const completed = turn.events.some((candidate) =>
      candidate.sequence > event.sequence &&
      (candidate.type === 'tool.completed' || candidate.type === 'tool.failed') &&
      eventString(candidate, 'name') === name)
    steps.push({
      id: `tool:${event.sequence}`,
      icon: Wrench,
      label: toolLabel(name),
      description: name,
      status: completed ? 'complete' : 'active',
    })
  }

  if (turn.status === 'completed') {
    for (const step of steps) step.status = 'complete'
  }
  return steps
}

function toolLabel(name: string) {
  if (name === 'canvas.search_context') return '搜索相关故事信息'
  if (name === 'canvas.get_nodes') return '读取画布节点'
  if (name === 'story_spine.search') return '检索故事脉络'
  return '调用创作工具'
}

function hasEvent(events: AgentEvent[], type: string) {
  return events.some((event) => event.type === type)
}

function lastEvent(events: AgentEvent[], type: string) {
  return events.findLast((event) => event.type === type)
}

function eventString(event: AgentEvent | undefined, key: string) {
  const value = event?.data?.[key]
  return typeof value === 'string' ? value : undefined
}

function eventNumber(event: AgentEvent | undefined, key: string) {
  const value = event?.data?.[key]
  return typeof value === 'number' ? value : undefined
}
