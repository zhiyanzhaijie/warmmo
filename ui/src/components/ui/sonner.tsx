import { Toaster as Sonner, type ToasterProps } from 'sonner'

export function Toaster(props: ToasterProps) {
  return (
    <Sonner
      position="top-center"
      offset={{ top: '4.5rem' }}
      mobileOffset={{ top: '7.75rem' }}
      closeButton
      expand={false}
      duration={8_000}
      toastOptions={{
        unstyled: true,
        classNames: {
          toast: 'group relative flex w-[min(24rem,calc(100vw-2rem))] items-start bg-canvas-elevated px-space-md py-space-sm text-body shadow-floating',
          title: 'pr-space-lg text-label-sm text-inherit',
          description: 'text-body-sm text-mute',
          error: 'text-error',
          icon: 'hidden',
          closeButton: 'absolute right-space-xs top-space-xs grid size-5 place-items-center bg-transparent text-mute outline-none hover:text-ink focus-visible:ring-1 focus-visible:ring-ring',
        },
      }}
      {...props}
    />
  )
}
