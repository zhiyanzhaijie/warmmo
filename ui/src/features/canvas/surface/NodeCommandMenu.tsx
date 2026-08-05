import { useEffect, useRef } from 'react'

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'
import {
  creatableNodeKinds,
  nodeDefinitions,
} from '@/features/canvas/nodes/definitions'
import type { CanvasNodeCommandMenuProps } from '@/features/canvas/surface/types'

export function CanvasNodeCommandMenu({
  disabled = false,
  label,
  open,
  onOpenChange,
  onSelect,
}: CanvasNodeCommandMenuProps) {
  const triggerRef = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    if (!open || triggerRef.current === null) return
    const trigger = triggerRef.current
    const frame = requestAnimationFrame(() => {
      const bounds = trigger.getBoundingClientRect()
      trigger.dispatchEvent(new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        button: 2,
        clientX: bounds.left + bounds.width / 2,
        clientY: bounds.top + bounds.height / 2,
        view: window,
      }))
    })
    return () => cancelAnimationFrame(frame)
  }, [open])

  return (
    <ContextMenu onOpenChange={onOpenChange}>
      <ContextMenuTrigger
        ref={triggerRef}
        aria-hidden="true"
        disabled={disabled}
        className="block size-px"
        onContextMenu={(event) => event.stopPropagation()}
      />
      <ContextMenuContent collisionPadding={12} className="w-64 p-space-xs">
        <ContextMenuLabel className="px-space-xs pb-space-xs pt-space-xxs text-label-sm text-mute">
          {label}
        </ContextMenuLabel>
        <ContextMenuGroup className="flex flex-col">
          {creatableNodeKinds.map((kind, index) => {
            const definition = nodeDefinitions[kind]
            const Icon = definition.icon
            const previousKind = creatableNodeKinds[index - 1]
            const startsCategory = previousKind !== undefined
              && nodeDefinitions[previousKind].category !== definition.category
            return (
              <ContextMenuItem
                key={kind}
                className={`${startsCategory ? 'mt-space-xxs' : ''} h-9 gap-space-xs px-space-xs py-0 text-body-md focus:bg-hairline-soft`}
                onSelect={() => onSelect(kind)}
              >
                <Icon aria-hidden="true" className="shrink-0" size={16} style={{ color: definition.accentColor }} />
                <span className="min-w-0 truncate text-ink">{definition.label}</span>
              </ContextMenuItem>
            )
          })}
        </ContextMenuGroup>
      </ContextMenuContent>
    </ContextMenu>
  )
}
