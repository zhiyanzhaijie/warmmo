import type { LucideIcon } from 'lucide-react'
import { BrainCircuit, Check, Circle, Compass, Feather, Search, Wrench } from 'lucide-react'
import { memo, useMemo } from 'react'

import {
  ChainOfThought,
  ChainOfThoughtContent,
  ChainOfThoughtHeader,
  ChainOfThoughtStep,
} from '@/components/ai-elements/chain-of-thought'
import {
  Plan,
  PlanAction,
  PlanContent,
  PlanDescription,
  PlanHeader,
  PlanTitle,
  PlanTrigger,
} from '@/components/ai-elements/plan'
import { Shimmer } from '@/components/ai-elements/shimmer'
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
    <div className="space-y-space-sm">
      <ChainOfThought
        key={turn.status === 'completed' ? 'completed' : 'active'}
        defaultOpen={turn.status !== 'completed'}
        className="space-y-0"
      >
        <ChainOfThoughtHeader className="min-h-8 gap-space-xs text-body-md text-mute [&>svg:first-child]:hidden [&>svg:last-child]:size-3">
          <span className="flex min-w-0 items-center gap-space-xs">
            <BrainCircuit aria-hidden="true" className={running ? 'size-4 text-link' : 'size-4'} />
            {running ? (
              <Shimmer
                as="span"
                className="truncate font-medium [--color-background:var(--color-ink)] [--color-muted-foreground:var(--color-mute)]"
                duration={1.5}
                spread={8}
              >
                {summary}
              </Shimmer>
            ) : <span className="truncate">{summary}</span>}
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
      <CollaborativeAgentPlan turn={turn} />
    </div>
  )
})

function CollaborativeAgentPlan({ turn }: { turn: CollaborativeTurn }) {
  const started = lastEvent(turn.events, 'plan.started')
  if (started === undefined) return null

  const completed = lastEvent(turn.events, 'plan.completed')
  const plan = eventObject(completed, 'plan')
  const brief = objectString(plan, 'brief') ?? objectString(plan, 'intent')
  const writerRequired = plan?.writerRequired === true
  const creatorStarted = hasRoleEvent(turn.events, 'role.started', 'creator')
  const creatorCompleted = hasRoleEvent(turn.events, 'role.completed', 'creator')
  const writerStarted = hasRoleEvent(turn.events, 'role.started', 'writer')
  const writerCompleted = hasRoleEvent(turn.events, 'role.completed', 'writer')
  const planSteps = [
    { label: '明确目标与上下文', complete: completed !== undefined, active: completed === undefined },
    { label: '生成创作结果', complete: creatorCompleted, active: creatorStarted && !creatorCompleted },
    ...(writerRequired ? [{ label: '统一文风并校验', complete: writerCompleted, active: writerStarted && !writerCompleted }] : []),
  ]

  return (
    <Plan
      key={turn.status === 'completed' ? 'completed' : 'active'}
      defaultOpen={turn.status !== 'completed'}
      isStreaming={completed === undefined}
      className="gap-space-sm rounded-sm border-hairline bg-transparent py-space-sm"
    >
      <PlanHeader className="grid-cols-[1fr_auto] gap-space-xs px-space-sm">
        <div className="min-w-0 space-y-1">
          <PlanTitle className="text-body-md font-medium">创作计划</PlanTitle>
          <PlanDescription className="line-clamp-2 text-body-sm">
            {brief ?? '正在拆解目标与执行步骤'}
          </PlanDescription>
        </div>
        <PlanAction><PlanTrigger aria-label="展开或收起创作计划" /></PlanAction>
      </PlanHeader>
      <PlanContent className="px-space-sm">
        <ol className="space-y-space-xs">
          {planSteps.map((step) => (
            <li key={step.label} className="flex min-w-0 items-center gap-space-xs text-body-sm text-body">
              {step.complete
                ? <Check aria-hidden="true" className="size-3.5 shrink-0 text-ink" />
                : <Circle aria-hidden="true" className={step.active ? 'size-3.5 shrink-0 text-link' : 'size-3.5 shrink-0 text-faint'} />}
              <span className={step.active ? 'text-ink' : undefined}>{step.label}</span>
            </li>
          ))}
        </ol>
      </PlanContent>
    </Plan>
  )
}

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

  const toolEvents = turn.events.filter((event) => {
    if (event.type !== 'tool.requested') return false
    return !isInternalControlTool(eventString(event, 'toolName'))
  })
  for (const event of toolEvents) {
    const name = eventString(event, 'toolName') ?? 'Agent Tool'
    const completed = turn.events.some((candidate) =>
      candidate.sequence > event.sequence &&
      (candidate.type === 'tool.completed' || candidate.type === 'tool.failed') &&
      eventString(candidate, 'toolName') === name)
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
  if (name === 'story_spine.context') return '检索故事脉络'
  if (name === 'workspace.search') return '搜索创作资料'
  return '调用创作工具'
}

function isInternalControlTool(name: string | undefined) {
  return name === 'submit_artifact' || name === 'delegate_agent' || name === 'ask_user'
}

function hasEvent(events: AgentEvent[], type: string) {
  return events.some((event) => event.type === type)
}

function lastEvent(events: AgentEvent[], type: string) {
  return events.findLast((event) => event.type === type)
}

function hasRoleEvent(events: AgentEvent[], type: string, role: string) {
  return events.some((event) => event.type === type && eventString(event, 'role') === role)
}

function eventObject(event: AgentEvent | undefined, key: string) {
  const value = event?.data?.[key]
  return typeof value === 'object' && value !== null ? value as Record<string, unknown> : undefined
}

function objectString(value: Record<string, unknown> | undefined, key: string) {
  const field = value?.[key]
  return typeof field === 'string' ? field : undefined
}

function eventString(event: AgentEvent | undefined, key: string) {
  const value = event?.data?.[key]
  return typeof value === 'string' ? value : undefined
}

function eventNumber(event: AgentEvent | undefined, key: string) {
  const value = event?.data?.[key]
  return typeof value === 'number' ? value : undefined
}
