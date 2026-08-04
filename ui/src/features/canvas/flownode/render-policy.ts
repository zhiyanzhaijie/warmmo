import type { FlowNodeDetailLevel } from '@/features/canvas/flownode/types'

export const flowNodeRenderThresholds = {
  full: 0.55,
  compact: 0.18,
} as const

export function getFlowNodeDetailLevel(zoom: number): FlowNodeDetailLevel {
  if (zoom >= flowNodeRenderThresholds.full) return 'full'
  if (zoom >= flowNodeRenderThresholds.compact) return 'compact'
  return 'marker'
}
