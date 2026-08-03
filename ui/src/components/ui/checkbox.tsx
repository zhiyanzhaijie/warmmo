import * as React from 'react'
import { CheckIcon } from 'lucide-react'
import { Checkbox as CheckboxPrimitive } from 'radix-ui'

import { cn } from '@/lib/utils'

function Checkbox({ className, ...props }: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        'peer grid size-4 shrink-0 place-items-center rounded-[4px] border border-input bg-canvas-elevated outline-none transition-colors focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-link-soft data-[state=checked]:border-link data-[state=checked]:bg-link data-[state=checked]:text-white disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator data-slot="checkbox-indicator"><CheckIcon className="size-3" strokeWidth={3} /></CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

export { Checkbox }
