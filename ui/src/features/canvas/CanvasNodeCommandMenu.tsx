import type { ReactNode } from 'react'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  creatableNodeKinds,
  nodeCategoryDefinitions,
  nodeDefinitions,
  type CanvasNodeKind,
} from '@/features/canvas/nodes/definitions'

export function CanvasNodeCommandMenu({
  anchor,
  disabled = false,
  open,
  onOpenChange,
  onSelect,
}: {
  anchor: ReactNode
  disabled?: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (kind: CanvasNodeKind) => void
}) {
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild disabled={disabled}>
        {anchor}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="center" sideOffset={10} className="w-72 rounded-sm p-space-xs">
        <DropdownMenuLabel className="flex items-center justify-between px-space-sm py-space-xs">
          <span className="text-label-sm">创建目标节点</span>
          <span className="font-mono text-mono-eyebrow text-faint">8 TYPES</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <div className="grid grid-cols-2 gap-1">
          {creatableNodeKinds.map((kind) => {
            const definition = nodeDefinitions[kind]
            const Icon = definition.icon
            return (
              <DropdownMenuItem
                key={kind}
                className="min-h-12 items-start rounded-sm px-space-sm py-space-sm"
                onSelect={() => onSelect(kind)}
              >
                <span className={`mt-0.5 grid size-6 place-items-center rounded-sm text-on-primary ${definition.accentClassName}`}>
                  <Icon size={13} />
                </span>
                <span className="min-w-0">
                  <span className="block text-label-sm text-ink">{definition.label}</span>
                  <span className="block truncate text-body-sm text-mute">
                    {nodeCategoryDefinitions[definition.category].label}
                  </span>
                </span>
              </DropdownMenuItem>
            )
          })}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
