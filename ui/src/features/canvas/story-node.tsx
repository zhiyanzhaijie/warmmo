import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'
import { Box, Castle, Clock3, FileText, Gem, MapPinned, ScrollText, UserRound, type LucideIcon } from 'lucide-react'
import { memo } from 'react'

import type { CanvasNodeKind } from '@/types/canvas'

export interface StoryNodeData extends Record<string, unknown> {
  sourceId: string
  sourceType: 'node' | 'candidate'
  kind: CanvasNodeKind
  title: string
  content: string
  revision: number
  layerId: string
  contextTags: string[]
}

export type StoryFlowNode = Node<StoryNodeData, 'story'>

interface NodeVisual {
  label: string
  icon: LucideIcon
  accent: string
}

export const nodeVisuals: Record<CanvasNodeKind, NodeVisual> = {
  character: { label: '角色', icon: UserRound, accent: 'bg-link' },
  setting: { label: '背景', icon: MapPinned, accent: 'bg-cyan' },
  item: { label: '物品', icon: Gem, accent: 'bg-warning' },
  plot: { label: '剧情', icon: ScrollText, accent: 'bg-violet' },
  world: { label: '世界观', icon: Castle, accent: 'bg-magenta' },
  chapter: { label: '章节', icon: FileText, accent: 'bg-ink' },
  timeline: { label: '时间线', icon: Clock3, accent: 'bg-pink' },
  note: { label: '笔记', icon: Box, accent: 'bg-mute' },
}

const StoryNode = memo(function StoryNode({ data, selected }: NodeProps<StoryFlowNode>) {
  const visual = nodeVisuals[data.kind]
  const Icon = visual.icon

  return (
    <article className={`group relative w-64 overflow-hidden rounded-sm border bg-canvas-elevated shadow-whisper transition-[border-color,box-shadow] ${selected ? 'border-link shadow-floating' : 'border-hairline'}`}>
      <div className={`absolute inset-x-0 top-0 h-0.5 ${visual.accent}`} />
      <header className="flex h-10 items-center justify-between border-b border-hairline px-space-sm">
        <span className="flex min-w-0 items-center gap-space-xs text-label-sm">
          <Icon className="shrink-0 text-mute" size={15} />
          <span className="truncate">{data.title}</span>
        </span>
        <span className="font-mono text-body-sm text-mute">{visual.label}</span>
      </header>
      <div className="max-h-44 overflow-hidden px-space-sm py-space-sm text-body-sm leading-5 text-body">
        <p className="line-clamp-7 whitespace-pre-wrap">{data.content}</p>
      </div>
      <footer className="flex h-8 items-center justify-between border-t border-hairline px-space-sm font-mono text-[0.6875rem] text-mute">
        <span>{data.sourceType === 'candidate' ? 'CANDIDATE' : `REV ${data.revision}`}</span>
        <span>{data.layerId}</span>
      </footer>
      <Handle className="!size-2 !border-canvas-elevated !bg-mute opacity-0 group-hover:opacity-100" type="target" position={Position.Left} />
      <Handle className="!size-2 !border-canvas-elevated !bg-mute opacity-0 group-hover:opacity-100" type="source" position={Position.Right} />
    </article>
  )
})

export const storyNodeTypes = { story: StoryNode }
