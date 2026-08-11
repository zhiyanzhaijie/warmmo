import { QueryClientProvider } from '@tanstack/react-query'
import { RefreshCw, RotateCcw } from 'lucide-react'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ErrorBoundary, type FallbackProps } from 'react-error-boundary'

import { Button } from '@/components/ui/button'
import { queryClient } from '@/lib/query/query-client'

import './styles/theme.css'
import { App } from './App'
import { ThemeProvider } from './components/theme/ThemeProvider'
import { Toaster } from './components/ui/sonner'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <ErrorBoundary fallbackRender={AppErrorFallback}>
          <App />
        </ErrorBoundary>
        <Toaster />
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)

function AppErrorFallback({ error, resetErrorBoundary }: FallbackProps) {
  const message = error instanceof Error ? error.message : '页面渲染失败'

  return (
    <main className="min-h-dvh bg-canvas px-space-lg text-ink">
      <section className="mx-auto flex min-h-dvh w-full max-w-[40rem] flex-col justify-center py-space-3xl">
        <div className="flex items-center gap-space-xs font-mono text-mono-eyebrow text-mute">
          <span aria-hidden="true" className="size-1.5 rounded-full bg-error" />
          WARMMO / RENDER ERROR
        </div>

        <div className="mt-space-lg border-t border-hairline pt-space-xl">
          <h1 className="text-heading-lg">页面未能完成渲染</h1>
          <p className="mt-space-sm max-w-lg text-body-md text-body">
            当前视图遇到异常。你可以重试渲染，或重新加载应用。
          </p>
          <p className="mt-space-lg border-l border-error pl-space-md font-mono text-body-sm text-mute" role="alert">
            {message}
          </p>
        </div>

        <div className="mt-space-xl flex flex-wrap items-center gap-space-sm">
          <Button onClick={() => window.location.reload()}>
            <RefreshCw aria-hidden="true" />
            重新加载
          </Button>
          <Button variant="outline" onClick={resetErrorBoundary}>
            <RotateCcw aria-hidden="true" />
            重试视图
          </Button>
        </div>
      </section>
    </main>
  )
}
