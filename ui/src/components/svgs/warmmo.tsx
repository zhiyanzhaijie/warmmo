import { useId } from 'react'

import { cn } from '@/lib/utils'

function Warmmo({ className, ...props }: React.ComponentProps<'svg'>) {
  return (
    <svg
      aria-hidden="true"
      className={cn('size-8', className)}
      viewBox="0 0 128 128"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <circle className="fill-primary" cx="64" cy="64" r="54" />
      <circle className="fill-background" cx="87" cy="87" r="15" />
    </svg>
  )
}

type WarmmoAnimatedProps = React.ComponentProps<'svg'> & {
  duration?: number
}

function WarmmoAnimated({ duration = 4800, className, ...props }: WarmmoAnimatedProps) {
  const clipPathId = useId()
  const animationDuration = `${duration}ms`

  return (
    <svg
      aria-hidden="true"
      className={cn('size-8', className)}
      viewBox="0 0 128 128"
      xmlns="http://www.w3.org/2000/svg"
      {...props}
    >
      <defs>
        <clipPath id={clipPathId}>
          <circle cx="64" cy="64" r="54" />
        </clipPath>
      </defs>

      <circle className="fill-primary" cx="64" cy="64" r="54" />

      <g clipPath={`url(#${clipPathId})`}>
        <circle className="fill-background" cx="87" cy="87" r="15">
          <animate attributeName="cx" calcMode="spline" dur={animationDuration} keySplines="0.4 0 0.2 1; 0.4 0 0.2 1; 0.4 0 0.2 1" keyTimes="0; 0.14; 0.42; 1" repeatCount="indefinite" values="87; 93; 87; 87" />
          <animate attributeName="cy" calcMode="spline" dur={animationDuration} keySplines="0.4 0 0.2 1; 0.4 0 0.2 1; 0.4 0 0.2 1" keyTimes="0; 0.14; 0.42; 1" repeatCount="indefinite" values="87; 76; 64; 64" />
          <animate attributeName="r" calcMode="spline" dur={animationDuration} keySplines="0.4 0 0.2 1; 0.65 0 0.35 1; 0.4 0 0.2 1" keyTimes="0; 0.14; 0.42; 1" repeatCount="indefinite" values="15; 15; 88; 88" />
        </circle>

        <circle className="fill-primary" cx="41" cy="64" r="0">
          <animate attributeName="cx" calcMode="spline" dur={animationDuration} keySplines="0.4 0 0.2 1; 0.4 0 0.2 1; 0.4 0 0.2 1" keyTimes="0; 0.42; 0.58; 1" repeatCount="indefinite" values="35; 35; 41; 41" />
          <animate attributeName="cy" calcMode="spline" dur={animationDuration} keySplines="0.4 0 0.2 1; 0.4 0 0.2 1; 0.4 0 0.2 1" keyTimes="0; 0.42; 0.58; 1" repeatCount="indefinite" values="52; 52; 64; 64" />
          <animate attributeName="r" calcMode="spline" dur={animationDuration} keySplines="0.4 0 0.2 1; 0.4 0 0.2 1; 0.65 0 0.35 1; 0.4 0 0.2 1" keyTimes="0; 0.42; 0.58; 0.86; 1" repeatCount="indefinite" values="0; 0; 15; 88; 88" />
        </circle>

        <circle className="fill-background" cx="87" cy="87" r="0">
          <animate attributeName="r" calcMode="spline" dur={animationDuration} keySplines="0.4 0 0.2 1; 0.4 0 0.2 1" keyTimes="0; 0.86; 1" repeatCount="indefinite" values="0; 0; 15" />
        </circle>
      </g>
    </svg>
  )
}

export { Warmmo, WarmmoAnimated }
