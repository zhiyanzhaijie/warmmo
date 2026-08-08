import { XIcon } from 'lucide-react'
import * as React from 'react'
import { Dialog as DrawerPrimitive } from 'radix-ui'

import { cn } from '@/lib/utils'

function Drawer(props: React.ComponentProps<typeof DrawerPrimitive.Root>) {
  return <DrawerPrimitive.Root data-slot="drawer" {...props} />
}

function DrawerTrigger(props: React.ComponentProps<typeof DrawerPrimitive.Trigger>) {
  return <DrawerPrimitive.Trigger data-slot="drawer-trigger" {...props} />
}

function DrawerPortal(props: React.ComponentProps<typeof DrawerPrimitive.Portal>) {
  return <DrawerPrimitive.Portal data-slot="drawer-portal" {...props} />
}

function DrawerOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.Overlay>) {
  return (
    <DrawerPrimitive.Overlay
      data-slot="drawer-overlay"
      className={cn(
        'fixed inset-0 z-50 bg-black/20 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0',
        className,
      )}
      {...props}
    />
  )
}

function DrawerContent({
  className,
  children,
  side = 'bottom',
  showCloseButton = true,
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.Content> & {
  side?: 'bottom' | 'right'
  showCloseButton?: boolean
}) {
  return (
    <DrawerPortal>
      <DrawerOverlay />
      <DrawerPrimitive.Content
        data-slot="drawer-content"
        className={cn(
          'fixed z-50 flex bg-canvas-elevated shadow-floating outline-none data-[state=closed]:animate-out data-[state=open]:animate-in',
          side === 'right'
            ? 'inset-y-0 right-0 h-dvh w-[min(32rem,calc(100vw-1rem))] flex-col border-l border-hairline data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right'
            : 'inset-x-0 bottom-0 max-h-[min(36rem,calc(100dvh-2rem))] w-full flex-col rounded-t-sm border-t border-hairline data-[state=closed]:slide-out-to-bottom data-[state=open]:slide-in-from-bottom',
          className,
        )}
        {...props}
      >
        {children}
        {showCloseButton ? (
          <DrawerPrimitive.Close
            className="absolute top-space-md right-space-md grid size-8 place-items-center rounded-sm text-mute transition-colors hover:bg-hairline-soft hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-link"
          >
            <XIcon aria-hidden="true" size={16} />
            <span className="sr-only">关闭</span>
          </DrawerPrimitive.Close>
        ) : null}
      </DrawerPrimitive.Content>
    </DrawerPortal>
  )
}

function DrawerHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="drawer-header"
      className={cn('shrink-0 border-b border-hairline px-space-lg py-space-md pr-16', className)}
      {...props}
    />
  )
}

function DrawerTitle({
  className,
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.Title>) {
  return (
    <DrawerPrimitive.Title
      data-slot="drawer-title"
      className={cn('text-heading-md text-ink', className)}
      {...props}
    />
  )
}

function DrawerDescription({
  className,
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.Description>) {
  return (
    <DrawerPrimitive.Description
      data-slot="drawer-description"
      className={cn('mt-space-xs text-body-sm text-mute', className)}
      {...props}
    />
  )
}

export {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerPortal,
  DrawerTitle,
  DrawerTrigger,
}
