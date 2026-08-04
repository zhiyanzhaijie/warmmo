import * as React from 'react'

import { cn } from '@/lib/utils'

function Input({ className, ...props }: React.ComponentProps<'input'>) {
  return (
    <input
      className={cn('h-10 w-full rounded-sm bg-hairline-soft px-space-sm text-body-md text-ink outline-none transition-shadow placeholder:text-faint focus:ring-2 focus:ring-link-soft disabled:cursor-not-allowed disabled:opacity-50', className)}
      {...props}
    />
  )
}

export { Input }
