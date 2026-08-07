import type { SourceDocumentUIPart } from 'ai'
import { PlusIcon, XIcon } from 'lucide-react'

import {
  Attachment,
  AttachmentHoverCard,
  AttachmentHoverCardContent,
  AttachmentHoverCardTrigger,
  AttachmentInfo,
  AttachmentPreview,
  AttachmentRemove,
  Attachments,
} from '@/components/ai-elements/attachments'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { CanvasContextNode } from '@/features/canvas/agent-workspace/types'
import { cn } from '@/lib/utils'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'

interface CanvasContextNodesProps {
  disabled: boolean
  isPicking: boolean
  nodes: CanvasContextNode[]
  onPickerToggle: () => void
  onRemove: (nodeId: string) => void
}

export function CanvasContextNodes({
  disabled,
  isPicking,
  nodes,
  onPickerToggle,
  onRemove,
}: CanvasContextNodesProps) {
  return (
    <div className="flex min-h-0 min-w-0 items-center gap-space-xs px-space-sm pt-space-sm">
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            aria-label={isPicking ? '取消从画布选择上下文节点' : '从画布选择上下文节点'}
            aria-pressed={isPicking}
            className={cn(
              'grid size-7 shrink-0 place-items-center rounded-sm p-0 text-mute transition-colors hover:bg-hairline-soft hover:text-ink focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-link disabled:cursor-not-allowed disabled:opacity-35',
              isPicking && 'bg-hairline-soft text-ink',
            )}
            disabled={disabled}
            type="button"
            onClick={onPickerToggle}
          >
            {isPicking ? <XIcon aria-hidden="true" size={14} /> : <PlusIcon aria-hidden="true" size={15} />}
          </button>
        </TooltipTrigger>
        <TooltipContent side="top">{isPicking ? '取消选择上下文' : '从画布选择上下文'}</TooltipContent>
      </Tooltip>
      <Attachments
        variant="inline"
        className="min-h-8 min-w-0 flex-1 flex-nowrap items-center gap-space-xxs overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {nodes.map((node) => {
          const definition = nodeDefinitions[node.kind]
          const ContextIcon = definition.icon
          const attachment: SourceDocumentUIPart & { id: string } = {
            id: node.id,
            type: 'source-document',
            sourceId: node.id,
            mediaType: `application/x-warmnote-${node.kind}`,
            title: node.title,
          }
          return (
            <AttachmentHoverCard key={node.id}>
              <AttachmentHoverCardTrigger asChild>
                <div className="shrink-0">
                  <Attachment
                    className="max-w-44"
                    data={attachment}
                    onRemove={() => onRemove(node.id)}
                  >
                    <AttachmentPreview
                      fallbackIcon={<ContextIcon aria-hidden="true" size={12} style={{ color: definition.accentColor }} />}
                    />
                    <AttachmentInfo />
                    <AttachmentRemove disabled={disabled} label={`移除上下文节点：${node.title}`} />
                  </Attachment>
                </div>
              </AttachmentHoverCardTrigger>
              <AttachmentHoverCardContent side="top">
                <div className="flex min-w-0 items-center gap-space-xs">
                  <ContextIcon aria-hidden="true" className="shrink-0" size={13} style={{ color: definition.accentColor }} />
                  <span className="min-w-0 flex-1 truncate text-label-sm text-ink">{node.title}</span>
                  <span className="shrink-0 font-mono text-[0.625rem] text-mute">{definition.label}</span>
                </div>
                <p className="mt-space-xs line-clamp-3 text-body-sm leading-4 text-mute">
                  {node.content || definition.description}
                </p>
              </AttachmentHoverCardContent>
            </AttachmentHoverCard>
          )
        })}
      </Attachments>
    </div>
  )
}
