import type { LucideIcon } from 'lucide-react'
import {
  CircleDashed,
  CircleX,
  Database,
  FileCheck2,
  ListChecks,
  LoaderCircle,
  MessageCircleQuestion,
  PenLine,
  RefreshCw,
  Sparkles,
  Wrench,
} from 'lucide-react'
import { memo, useMemo } from 'react'

import {
  ChainOfThought,
  ChainOfThoughtContent,
  ChainOfThoughtHeader,
  ChainOfThoughtStep,
} from '@/components/ai-elements/chain-of-thought'
import {
  type NodeAgentRunState,
  useFlowNodeStore,
} from '@/features/canvas/flownode/store'
import type { AgentEvent } from '@/types/canvas'

type ExecutionStepStatus = 'complete' | 'active' | 'pending' | 'error'

interface ExecutionStep {
  id: string
  icon: LucideIcon
  label: string
  description?: string
  status: ExecutionStepStatus
}

interface ToolExecutionStep extends ExecutionStep {
  name: string
}

export const NodeAgentExecution = memo(function NodeAgentExecution({ nodeId }: { nodeId: string }) {
  const run = useFlowNodeStore((state) => state.nodeAgentRuns[nodeId])
  const steps = useMemo(() => run === undefined ? [] : buildExecutionSteps(run), [run])

  if (run === undefined || run.status === 'completed') return null

  if (run.status === 'failed') {
    return (
      <span
        aria-label="节点更新失败"
        className="nodrag nopan absolute top-2 right-2 z-10 grid size-6 place-items-center rounded-sm bg-canvas-elevated text-error shadow-whisper"
        title={run.error || '节点更新失败'}
      >
        <CircleX aria-hidden="true" size={14} />
      </span>
    )
  }

  const summary = executionSummary(run, steps)
  const isWaiting = run.status === 'waiting_input'

  return (
    <div className="nodrag nopan nowheel absolute inset-0 z-10 overflow-hidden bg-canvas-elevated/95 px-space-xs py-space-xxs backdrop-blur-[1px]">
      <ChainOfThought defaultOpen className="flex h-full flex-col space-y-0">
        <ChainOfThoughtHeader className="min-h-5 gap-1 text-[11px] leading-4 [&>svg:first-child]:hidden [&>svg:last-child]:size-3">
          <span className="flex min-w-0 items-center gap-space-xxs">
            {isWaiting ? <MessageCircleQuestion
              aria-hidden="true"
              className="size-3 shrink-0 text-link"
            /> : <LoaderCircle
              aria-hidden="true"
              className="size-3 shrink-0 animate-spin text-link"
            />}
            <span className="truncate">{summary}</span>
          </span>
        </ChainOfThoughtHeader>
        <ChainOfThoughtContent className="mt-1 max-h-full min-h-0 space-y-1 overflow-y-auto pb-1">
          {steps.map((step) => (
            <ChainOfThoughtStep
              key={step.id}
              icon={step.icon}
              label={step.label}
              description={step.description}
              status={step.status === 'error' ? 'complete' : step.status}
              className={`gap-1 text-[11px] leading-3.5 [&_svg]:size-3 [&>div:first-child]:mt-px [&>div:first-child>div]:top-5 [&>div:last-child]:space-y-0.5 [&>div:last-child>div:nth-child(2)]:text-[10px] [&>div:last-child>div:nth-child(2)]:leading-3 ${step.status === 'error' ? 'text-error' : ''}`}
            />
          ))}
        </ChainOfThoughtContent>
      </ChainOfThought>
    </div>
  )
})

export const NodeAgentCompactIndicator = memo(function NodeAgentCompactIndicator({ nodeId }: { nodeId: string }) {
  const run = useFlowNodeStore((state) => state.nodeAgentRuns[nodeId])
  if (run === undefined || run.status === 'completed') return null

  const isActive = run.status === 'submitting' || run.status === 'running'
  const Icon = run.status === 'waiting_input' ? MessageCircleQuestion : isActive ? LoaderCircle : CircleX
  const label = run.status === 'failed' ? '节点更新失败' : run.status === 'waiting_input' ? '等待你的回答' : '节点更新中'

  return (
    <span
      aria-label={label}
      className={`absolute top-1 right-1 grid size-6 place-items-center rounded-sm bg-canvas-elevated shadow-whisper ${run.status === 'failed' ? 'text-error' : 'text-link'}`}
      title={label}
    >
      <Icon aria-hidden="true" className={isActive ? 'animate-spin' : undefined} size={13} />
    </span>
  )
})

export const NodeAgentMarkerIndicator = memo(function NodeAgentMarkerIndicator({ nodeId }: { nodeId: string }) {
  const run = useFlowNodeStore((state) => state.nodeAgentRuns[nodeId])
  if (run === undefined || run.status === 'completed') return null

  const isActive = run.status === 'submitting' || run.status === 'running'
  const label = run.status === 'failed' ? '节点更新失败' : run.status === 'waiting_input' ? '等待你的回答' : '节点更新中'

  return (
    <span
      aria-label={label}
      className={`pointer-events-none absolute -inset-1 rounded-full border-2 ${isActive ? 'animate-pulse border-link' : run.status === 'waiting_input' ? 'border-link' : 'border-error'}`}
      title={label}
    />
  )
})

function buildExecutionSteps(run: NodeAgentRunState): ExecutionStep[] {
  if (run.events.length === 0) {
    return [{
      id: 'submit',
      icon: CircleDashed,
      label: run.status === 'failed' ? '提交更新失败' : '提交节点更新',
      description: run.error || undefined,
      status: run.status === 'failed' ? 'error' : 'active',
    }]
  }

  const steps: ExecutionStep[] = []
  const contextPreparing = lastEvent(run.events, 'context.preparing')
  const contextReady = lastEvent(run.events, 'context.ready')
  if (contextPreparing !== undefined || contextReady !== undefined) {
    const nodeCount = eventNumber(contextReady ?? contextPreparing, 'nodeCount')
    steps.push({
      id: 'context',
      icon: Database,
      label: '读取画布上下文',
      description: nodeCount === undefined ? undefined : `${nodeCount} 个节点`,
      status: contextReady === undefined ? 'active' : 'complete',
    })
  }

  const skillSearching = lastEvent(run.events, 'skill.searching')
  const skillLoaded = lastEvent(run.events, 'skill.loaded')
  if (skillSearching !== undefined || skillLoaded !== undefined) {
    const skillId = eventString(skillLoaded, 'skillId')
    steps.push({
      id: 'skill',
      icon: Sparkles,
      label: '选择节点更新能力',
      description: skillId,
      status: skillLoaded === undefined ? 'active' : 'complete',
    })
  }

  const actionSteps = buildActionSteps(run.events)
  steps.push(...actionSteps)

  const approvalRequired = lastEvent(run.events, 'approval.required')
  if (approvalRequired !== undefined) {
    steps.push({
      id: `approval:${approvalRequired.sequence}`,
      icon: MessageCircleQuestion,
      label: '等待你的回答',
      description: eventString(approvalRequired, 'question'),
      status: run.status === 'waiting_input' ? 'active' : 'complete',
    })
  }

  const generationStarted = lastEvent(run.events, 'generation.started')
  const contentReady = lastEvent(run.events, 'message.delta')
  if (generationStarted !== undefined) {
    steps.push({
      id: 'generation',
      icon: PenLine,
      label: '生成节点内容',
      description: contentReady === undefined ? '等待模型返回结构化内容' : '内容生成完成',
      status: contentReady === undefined ? 'active' : 'complete',
    })
  } else if (run.status === 'running' && !steps.some((step) => step.status === 'active')) {
    steps.push({
      id: 'next-action',
      icon: CircleDashed,
      label: '分析下一步操作',
      status: 'active',
    })
  }

  const validationCompleted = lastEvent(run.events, 'validation.completed')
  const nodeUpdated = lastEvent(run.events, 'node.updated')
  if (validationCompleted !== undefined || nodeUpdated !== undefined || run.status === 'completed') {
    steps.push({
      id: 'apply',
      icon: FileCheck2,
      label: '校验并更新节点',
      status: nodeUpdated !== undefined || run.status === 'completed' ? 'complete' : 'active',
    })
  }

  if (run.status === 'failed') markLastActiveStepFailed(steps, run.error)
  if (run.status === 'completed') completeActiveSteps(steps)
  return steps
}

function buildActionSteps(events: AgentEvent[]) {
  const steps: Array<ExecutionStep | ToolExecutionStep> = []

  for (const event of events) {
    if (event.type === 'tool.requested') {
      const name = eventString(event, 'name') ?? 'unknown'
      steps.push({
        id: `tool:${event.sequence}`,
        icon: Wrench,
        name,
        label: toolLabel(name),
        description: toolRequestDescription(event, name),
        status: 'active',
      })
      continue
    }

    if (event.type === 'tool.started' || event.type === 'tool.completed' || event.type === 'tool.failed') {
      const name = eventString(event, 'name') ?? 'unknown'
      const step = findPendingToolStep(steps, name)
      if (step === undefined) continue
      if (event.type === 'tool.started') {
        step.description = `${name} · 正在调用`
      } else if (event.type === 'tool.completed') {
        step.status = 'complete'
        step.description = `${name} · 已返回结果`
      } else if (event.type === 'tool.failed') {
        step.status = 'error'
        step.description = eventString(event, 'message') ?? `${name} · 调用失败`
      }
      continue
    }

    if (event.type === 'brainstorm.completed') {
      steps.push({
        id: `brainstorm:${event.sequence}`,
        icon: Sparkles,
        label: '整理创作思路',
        description: truncate(eventString(event, 'summary')),
        status: 'complete',
      })
      continue
    }

    if (event.type === 'plan.completed') {
      steps.push({
        id: `plan:${event.sequence}`,
        icon: ListChecks,
        label: '制定节点更新计划',
        description: truncate(eventString(event, 'plan')),
        status: 'complete',
      })
      continue
    }

    if (event.type === 'decision.invalid') {
      const attempt = eventNumber(event, 'attempt')
      steps.push({
        id: `repair:${event.sequence}`,
        icon: RefreshCw,
        label: '修复模型响应格式',
        description: attempt === undefined ? eventString(event, 'message') : `第 ${attempt} 次 · ${eventString(event, 'message') ?? '格式无效'}`,
        status: 'complete',
      })
    }
  }

  return steps
}

function findPendingToolStep(steps: Array<ExecutionStep | ToolExecutionStep>, name: string) {
  for (let index = steps.length - 1; index >= 0; index -= 1) {
    const step = steps[index]
    if ('name' in step && step.name === name && step.status === 'active') return step
  }
  return undefined
}

function executionSummary(run: NodeAgentRunState, steps: ExecutionStep[]) {
  if (run.status === 'failed') return '节点更新失败'
  if (run.status === 'completed') return '节点更新完成'
  if (run.status === 'waiting_input') return '等待你的回答'
  const activeStep = steps.findLast((step) => step.status === 'active')
  return activeStep?.label ?? '正在更新节点'
}

function markLastActiveStepFailed(steps: ExecutionStep[], error: string) {
  const activeStep = steps.findLast((step) => step.status === 'active')
  if (activeStep !== undefined) {
    activeStep.status = 'error'
    activeStep.description = error || activeStep.description
    return
  }
  steps.push({ id: 'failed', icon: CircleX, label: '节点更新失败', description: error, status: 'error' })
}

function completeActiveSteps(steps: ExecutionStep[]) {
  for (const step of steps) {
    if (step.status === 'active') step.status = 'complete'
  }
}

function toolLabel(name: string) {
  switch (name) {
    case 'canvas.get_nodes':
      return '读取画布节点'
    case 'canvas.create_candidate':
      return '创建候选节点'
    default:
      return '调用 Agent 工具'
  }
}

function toolRequestDescription(event: AgentEvent, name: string) {
  const args = event.data?.arguments
  if (typeof args === 'object' && args !== null && 'nodeIds' in args && Array.isArray(args.nodeIds)) {
    return `${name} · 请求 ${args.nodeIds.length} 个节点`
  }
  return name
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

function truncate(value: string | undefined) {
  if (value === undefined || value.length <= 72) return value
  return `${value.slice(0, 72)}...`
}
