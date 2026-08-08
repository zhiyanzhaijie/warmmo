import * as React from 'react'
import { XIcon } from 'lucide-react'
import { Drawer as DrawerPrimitive } from 'vaul'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

function Drawer({
  shouldScaleBackground = false,
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.Root>) {
  return (
    <DrawerPrimitive.Root
      data-slot="drawer"
      shouldScaleBackground={shouldScaleBackground}
      {...props}
    />
  )
}

function DrawerTrigger(props: React.ComponentProps<typeof DrawerPrimitive.Trigger>) {
  return <DrawerPrimitive.Trigger data-slot="drawer-trigger" {...props} />
}

function DrawerPortal(props: React.ComponentProps<typeof DrawerPrimitive.Portal>) {
  return <DrawerPrimitive.Portal data-slot="drawer-portal" {...props} />
}

function DrawerClose(props: React.ComponentProps<typeof DrawerPrimitive.Close>) {
  return <DrawerPrimitive.Close data-slot="drawer-close" {...props} />
}

function DrawerOverlay({
  className,
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.Overlay>) {
  return (
    <DrawerPrimitive.Overlay
      data-slot="drawer-overlay"
      className={cn('fixed inset-0 z-50 bg-black/20', className)}
      {...props}
    />
  )
}

function DrawerHandle({
  className,
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.Handle>) {
  return (
    <DrawerPrimitive.Handle
      data-slot="drawer-handle"
      className={cn('!h-1 !w-20 !rounded-full !bg-hairline transition-colors hover:!bg-mute', className)}
      {...props}
    />
  )
}

interface DrawerContentProps extends React.ComponentProps<typeof DrawerPrimitive.Content> {
  defaultWidth?: number
  maxWidth?: number
  minWidth?: number
  onHandleClose?: () => void
  resizable?: boolean
  showCloseButton?: boolean
  showHandle?: boolean
  showOverlay?: boolean
}

function DrawerContent({
  className,
  children,
  defaultWidth = 512,
  maxWidth = 768,
  minWidth = 384,
  onHandleClose,
  resizable = false,
  showCloseButton = true,
  showHandle = false,
  showOverlay = true,
  style,
  ...props
}: DrawerContentProps) {
  const [width, setWidth] = React.useState(() => clampDrawerWidth(defaultWidth, minWidth, maxWidth))
  const dragStateRef = React.useRef<{ startWidth: number; startX: number } | null>(null)

  const updateWidth = React.useCallback((nextWidth: number) => {
    const viewportMaximum = typeof window === 'undefined' ? maxWidth : Math.max(0, window.innerWidth - 16)
    const effectiveMaximum = Math.min(maxWidth, viewportMaximum)
    const effectiveMinimum = Math.min(minWidth, effectiveMaximum)
    setWidth(Math.min(effectiveMaximum, Math.max(effectiveMinimum, nextWidth)))
  }, [maxWidth, minWidth])

  const beginResize = React.useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    dragStateRef.current = { startWidth: width, startX: event.clientX }
  }, [width])

  const resize = React.useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const dragState = dragStateRef.current
    if (dragState === null) return
    updateWidth(dragState.startWidth + dragState.startX - event.clientX)
  }, [updateWidth])

  const endResize = React.useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (dragStateRef.current === null) return
    dragStateRef.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
  }, [])

  const resizeWithKeyboard = React.useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      updateWidth(width + 16)
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      updateWidth(width - 16)
    } else if (event.key === 'Home') {
      event.preventDefault()
      updateWidth(minWidth)
    } else if (event.key === 'End') {
      event.preventDefault()
      updateWidth(maxWidth)
    }
  }, [maxWidth, minWidth, updateWidth, width])

  return (
    <DrawerPortal>
      {showOverlay ? <DrawerOverlay /> : null}
      <DrawerPrimitive.Content
        data-slot="drawer-content"
        className={cn(
          'fixed z-50 flex bg-canvas-elevated shadow-floating outline-none',
          'data-[vaul-drawer-direction=bottom]:inset-x-0 data-[vaul-drawer-direction=bottom]:bottom-0 data-[vaul-drawer-direction=bottom]:max-h-[min(36rem,calc(100dvh-2rem))] data-[vaul-drawer-direction=bottom]:w-full data-[vaul-drawer-direction=bottom]:flex-col data-[vaul-drawer-direction=bottom]:rounded-t-sm data-[vaul-drawer-direction=bottom]:border-t data-[vaul-drawer-direction=bottom]:border-hairline',
          'data-[vaul-drawer-direction=right]:inset-y-0 data-[vaul-drawer-direction=right]:right-0 data-[vaul-drawer-direction=right]:h-dvh data-[vaul-drawer-direction=right]:w-[min(32rem,calc(100vw-1rem))] data-[vaul-drawer-direction=right]:flex-col data-[vaul-drawer-direction=right]:border-l data-[vaul-drawer-direction=right]:border-hairline',
          className,
        )}
        style={resizable ? { ...style, width: `min(${width}px, calc(100vw - 1rem))` } : style}
        {...props}
      >
        {resizable ? (
          <div
            aria-label="调整抽屉宽度"
            aria-orientation="vertical"
            aria-valuemax={maxWidth}
            aria-valuemin={minWidth}
            aria-valuenow={Math.round(width)}
            className="group absolute inset-y-0 left-0 z-10 w-5 -translate-x-1/2 cursor-col-resize touch-none outline-none"
            role="separator"
            tabIndex={0}
            onKeyDown={resizeWithKeyboard}
            onPointerCancel={endResize}
            onPointerDown={beginResize}
            onPointerMove={resize}
            onPointerUp={endResize}
          >
            <span className="absolute inset-y-0 left-1/2 w-1 -translate-x-1/2 bg-hairline transition-colors group-hover:bg-mute group-focus-visible:bg-mute" />
          </div>
        ) : null}
        {showHandle ? (
          <div className="flex h-6 shrink-0 items-center justify-center">
            <DrawerHandle
              aria-hidden={false}
              aria-label="收起抽屉"
              role="button"
              tabIndex={0}
              onKeyDown={(event) => {
                if (event.key !== 'Enter' && event.key !== ' ') return
                event.preventDefault()
                onHandleClose?.()
              }}
            />
          </div>
        ) : null}
        {children}
        {showCloseButton ? (
          <DrawerClose asChild>
            <Button
              aria-label="关闭"
              className="absolute top-space-md right-space-md text-mute hover:bg-hairline-soft hover:text-ink"
              size="icon-sm"
              variant="ghost"
            >
              <XIcon aria-hidden="true" size={16} />
            </Button>
          </DrawerClose>
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

function clampDrawerWidth(width: number, minWidth: number, maxWidth: number) {
  return Math.min(maxWidth, Math.max(minWidth, width))
}

export {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerDescription,
  DrawerHandle,
  DrawerHeader,
  DrawerOverlay,
  DrawerPortal,
  DrawerTitle,
  DrawerTrigger,
}
