import { memo } from 'react'

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'

export const CanvasContextNodes = memo(function CanvasContextNodes({ nodes }: { nodes: StoryFlowNode[] }) {
  if (nodes.length === 0) return null

  return (
    <div className="flex h-10 min-w-0 items-center gap-space-xs px-space-md">
      <span className="shrink-0 font-mono text-mono-eyebrow text-mute">CONTEXT</span>
      <TooltipProvider>
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto py-1">
          {nodes.map((node) => {
            const definition = nodeDefinitions[node.data.kind]
            const ContextIcon = definition.icon
            return (
              <Tooltip key={node.id}>
                <TooltipTrigger asChild>
                  <button
                    aria-label={`上下文节点：${node.data.title}`}
                    className="grid size-7 shrink-0 cursor-default place-items-center rounded-full border-0 outline-none transition-transform hover:scale-105 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-link"
                    style={{
                      color: definition.accentColor,
                      backgroundColor: `color-mix(in srgb, ${definition.accentColor} 12%, var(--color-hairline-soft))`,
                    }}
                    type="button"
                  >
                    <ContextIcon aria-hidden="true" size={13} />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="top" align="start" className="w-64 p-space-sm">
                  <div className="flex min-w-0 items-center gap-space-xs">
                    <ContextIcon aria-hidden="true" className="shrink-0" size={13} style={{ color: definition.accentColor }} />
                    <span className="min-w-0 flex-1 truncate text-label-sm text-ink">{node.data.title}</span>
                    <span className="shrink-0 font-mono text-[0.625rem] text-mute">{definition.label}</span>
                  </div>
                  <p className="mt-space-xs line-clamp-3 text-body-sm leading-4 text-mute">
                    {node.data.content || definition.description}
                  </p>
                </TooltipContent>
              </Tooltip>
            )
          })}
        </div>
      </TooltipProvider>
      <span className="shrink-0 font-mono text-body-sm text-faint">{nodes.length}</span>
    </div>
  )
})
