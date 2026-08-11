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
  'tool.requested': '请求 Tool',
  'tool.started': '调用 Tool',
  'tool.completed': 'Tool 完成',
  'tool.failed': 'Tool 失败',
  'approval.required': '等待用户确认',
  'user.response.received': '已收到用户回答',
  'run.resumed': '继续运行',
  'generation.started': '开始生成',
  'validation.completed': '校验完成',
  'candidate.created': '创建候选',
  'candidate.decision': '候选已处理',
  'node.updated': '节点已更新',
  'nodes.created': '派生节点已创建',
  'run.completed': '运行完成',
  'run.failed': '运行失败',
  'run.cancelled': '运行取消',
  'role.started': 'Agent 开始工作',
  'role.handoff': 'Agent 交接',
  'role.completed': 'Agent 完成工作',
  'projection.pending': '准备写入画布',
  'projection.retry_scheduled': '等待重试写入',
}

export const streamedAgentEventTypes = [...Object.keys(agentEventLabels), 'message.delta']
const terminalAgentEventTypes = new Set(['run.completed', 'run.failed', 'run.cancelled'])

export function isTerminalAgentEvent(type: string) {
  return terminalAgentEventTypes.has(type)
}

export function getAgentEventSummary(event: AgentEvent) {
  if (event.data === null) return `#${event.sequence}`
  const value = event.data.nodeId ?? event.data.candidateId ?? event.data.artifactId ?? event.data.skillId ?? event.data.name ?? event.data.snapshotId
  return typeof value === 'string' ? value : `#${event.sequence}`
}
