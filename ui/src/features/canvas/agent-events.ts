import type { AgentEvent } from '@/types/canvas'

export const agentEventLabels: Record<string, string> = {
  'run.queued': '进入队列',
  'run.started': '开始运行',
  'context.preparing': '准备上下文',
  'context.ready': '上下文就绪',
  'brainstorm.started': '开始构思',
  'brainstorm.completed': '完成构思',
  'plan.started': '开始规划',
  'plan.completed': '规划完成',
  'skill.searching': '检索 Skill',
  'skill.matched': '命中 Skill',
  'skill.loaded': '加载 Skill',
  'skill.completed': 'Skill 完成',
  'decision.invalid': '修复决策格式',
  'tool.requested': '请求 Tool',
  'tool.started': '调用 Tool',
  'tool.completed': 'Tool 完成',
  'tool.failed': 'Tool 失败',
  'approval.required': '等待用户确认',
  'validation.completed': '校验完成',
  'candidate.created': '创建候选',
  'node.updated': '节点已更新',
  'run.completed': '运行完成',
  'run.failed': '运行失败',
  'run.cancelled': '运行取消',
}

export const streamedAgentEventTypes = [...Object.keys(agentEventLabels), 'message.delta']

export function isTerminalAgentEvent(type: string) {
  return type === 'run.completed' || type === 'run.failed' || type === 'run.cancelled'
}

export function getAgentEventSummary(event: AgentEvent) {
  if (event.data === null) return `#${event.sequence}`
  const value = event.data.nodeId ?? event.data.candidateId ?? event.data.skillId ?? event.data.name ?? event.data.snapshotId
  return typeof value === 'string' ? value : `#${event.sequence}`
}
