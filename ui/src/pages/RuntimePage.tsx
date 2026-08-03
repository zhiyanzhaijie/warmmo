import type { ReactNode } from 'react'
import { ArrowDownUp, Cpu, RefreshCw, Server, Timer, Waypoints, WifiOff } from 'lucide-react'

import { useRuntimeInfo } from '@/apis/runtime-apis'

import type { RuntimeInfo } from '../types/runtime'

const dateFormatter = new Intl.DateTimeFormat('zh-CN', {
  dateStyle: 'medium',
  timeStyle: 'medium',
})

export function RuntimePage() {
  const { state, isRefreshing, reload } = useRuntimeInfo()
  const isLoading = state.status === 'loading' || isRefreshing

  return (
    <main className="min-h-dvh bg-canvas">
      <header className="border-b border-hairline bg-canvas">
        <div className="mx-auto flex h-16 max-w-app items-center justify-between px-space-md sm:px-space-lg">
          <a className="flex items-center gap-space-sm text-label-sm text-ink no-underline" href="/" aria-label="Warmnote 首页">
            <span className="grid size-8 place-items-center rounded-sm bg-primary text-on-primary">
              <Waypoints size={17} aria-hidden="true" />
            </span>
            <span>Warmnote</span>
          </a>
          <span className="font-mono text-mono-eyebrow uppercase text-mute">Local Runtime</span>
        </div>
      </header>

      <section className="mx-auto max-w-app px-space-md py-space-2xl sm:px-space-lg sm:py-space-3xl" aria-labelledby="page-title">
        <div className="mb-space-xl max-w-2xl">
          <p className="mb-space-xs font-mono text-mono-eyebrow uppercase text-mute">Connection</p>
          <h1 id="page-title" className="m-0 text-heading-lg text-ink">本地服务连接</h1>
          <p className="mt-space-sm text-body-lg text-body">Warmnote Core 的实时运行状态与 I/O 响应。</p>
        </div>

        <div className="overflow-hidden rounded-md border border-hairline bg-canvas-elevated shadow-whisper">
          <div className="flex min-h-20 items-center justify-between gap-space-md border-b border-hairline px-space-md py-space-sm sm:px-space-lg">
            <div className="min-w-0">
              <p className="m-0 truncate font-mono text-mono-eyebrow uppercase text-mute">GET /api/v1/runtime</p>
              <h2 className="mt-space-xxs text-heading-md text-ink">Runtime I/O</h2>
            </div>
            <button
              className="grid size-11 shrink-0 cursor-pointer place-items-center rounded-sm border border-hairline bg-canvas-elevated text-ink transition-colors hover:bg-hairline-soft disabled:cursor-wait disabled:text-faint"
              type="button"
              onClick={reload}
              disabled={isLoading}
              title="重新请求"
            >
              <RefreshCw aria-hidden="true" className={isLoading ? 'animate-spin' : undefined} size={16} />
              <span className="sr-only">重新请求</span>
            </button>
          </div>

          {state.status === 'loading' ? <LoadingState /> : null}
          {state.status === 'error' ? <ErrorState message={state.message} /> : null}
          {state.status === 'success' ? <SuccessState data={state.data} latencyMs={state.latencyMs} /> : null}
        </div>
      </section>
    </main>
  )
}

function SuccessState({ data, latencyMs }: { data: RuntimeInfo; latencyMs: number }) {
  return (
    <div aria-live="polite">
      <div className="flex items-center gap-space-xs border-b border-hairline bg-hairline-soft px-space-md py-space-sm text-label-sm sm:px-space-lg">
        <span className="size-2 rounded-full bg-link" />
        <span>Core 已连接</span>
        <span className="ml-auto font-mono text-body-sm text-link">200 OK</span>
      </div>

      <dl className="m-0 grid grid-cols-1 gap-px bg-hairline sm:grid-cols-2 lg:grid-cols-4">
        <Metric icon={<Server size={16} />} label="服务" value={`${data.name} v${data.version}`} />
        <Metric icon={<Cpu size={16} />} label="运行环境" value={`${data.goVersion} · ${data.platform}`} />
        <Metric icon={<Timer size={16} />} label="往返耗时" value={`${latencyMs} ms`} />
        <Metric icon={<ArrowDownUp size={16} />} label="请求标识" value={data.requestId} mono />
      </dl>

      <div className="border-t border-hairline p-space-md sm:p-space-lg">
        <div className="overflow-hidden rounded-md border border-hairline bg-hairline-soft">
          <div className="flex min-h-10 flex-col justify-center gap-space-xxs border-b border-hairline px-space-md py-space-xs font-mono text-body-sm text-mute sm:flex-row sm:items-center sm:justify-between">
            <span className="uppercase">JSON Response</span>
            <time dateTime={data.serverTime}>{dateFormatter.format(new Date(data.serverTime))}</time>
          </div>
          <pre className="m-0 overflow-auto p-space-md font-mono text-code text-body">{JSON.stringify(data, null, 2)}</pre>
        </div>
      </div>
    </div>
  )
}

function Metric({ icon, label, value, mono = false }: { icon: ReactNode; label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0 bg-canvas-elevated px-space-md py-space-lg sm:px-space-lg">
      <dt className="flex items-center gap-space-xs text-body-sm text-mute">{icon}<span>{label}</span></dt>
      <dd className={`mt-space-xs [overflow-wrap:anywhere] text-label-sm text-ink ${mono ? 'font-mono' : ''}`}>{value}</dd>
    </div>
  )
}

function LoadingState() {
  return (
    <div className="flex min-h-80 flex-col items-center justify-center gap-space-sm p-space-lg text-center text-body-md text-mute" aria-live="polite">
      <RefreshCw className="animate-spin text-link" size={20} aria-hidden="true" />
      <p>正在读取本地 Runtime...</p>
    </div>
  )
}

function ErrorState({ message }: { message: string }) {
  return (
    <div className="flex min-h-80 flex-col items-center justify-center p-space-lg text-center" role="alert">
      <span className="grid size-11 place-items-center rounded-full bg-hairline-soft text-error">
        <WifiOff size={20} aria-hidden="true" />
      </span>
      <strong className="mt-space-sm text-label-sm text-ink">无法连接本地 Core</strong>
      <p className="mt-space-xs text-body-md text-body">{message}</p>
    </div>
  )
}
