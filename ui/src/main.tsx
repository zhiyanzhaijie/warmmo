import { QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ErrorBoundary } from 'react-error-boundary'
import { toast } from 'sonner'

import { queryClient } from '@/lib/query/query-client'

import './styles/theme.css'
import { App } from './App'
import { ThemeProvider } from './components/theme/ThemeProvider'
import { Toaster } from './components/ui/sonner'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <ErrorBoundary fallback={null} onError={(error) => toast.error(error instanceof Error ? error.message : '页面渲染失败')}>
          <App />
        </ErrorBoundary>
        <Toaster />
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)
