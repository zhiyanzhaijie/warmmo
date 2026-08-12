import type { ReactNode } from 'react'
import { Check, Cpu, Download, ExternalLink, RefreshCw, Server, Terminal, Timer, WifiOff } from 'lucide-react'

import { useRuntimeInfo } from '@/apis/runtime-apis'
import { Button } from '@/components/ui/button'

import type { RuntimeInfo } from '../types/runtime'

const releasesURL = 'https://github.com/zhiyanzhaijie/warmmo/releases/latest'

export function RuntimePage() {
  const { state, isRefreshing, reload } = useRuntimeInfo()
  const isLoading = state.status === 'loading' || isRefreshing

  return (
    <main className="min-h-[calc(100dvh-4rem)] bg-canvas text-ink">
      <section className="mx-auto max-w-app px-space-lg py-space-3xl sm:py-space-4xl" aria-labelledby="runtime-title">
        <div className="flex items-start justify-between gap-space-lg border-b border-hairline pb-space-lg">
          <div>
            <h1 id="runtime-title" className="mt-space-xs text-heading-lg">本地 Core</h1>
          </div>
          <button
            className="grid size-10 shrink-0 cursor-pointer place-items-center rounded-sm text-mute transition-colors hover:bg-hairline-soft hover:text-ink disabled:cursor-wait disabled:text-faint"
            type="button"
            onClick={reload}
            disabled={isLoading}
            title="重新检测"
            aria-label="重新检测本地 Core"
          >
            <RefreshCw aria-hidden="true" className={isLoading ? 'animate-spin' : undefined} size={16} />
          </button>
        </div>

        {state.status === 'loading' ? <LoadingState /> : null}
        {state.status === 'error' ? <ErrorState message={state.message} isRefreshing={isRefreshing} reload={reload} /> : null}
        {state.status === 'success' ? <SuccessState data={state.data} latencyMs={state.latencyMs} /> : null}
      </section>
    </main>
  )
}

function SuccessState({ data, latencyMs }: { data: RuntimeInfo; latencyMs: number }) {
  return (
    <div className="pt-space-xl" aria-live="polite">
      <div className="flex items-center gap-space-xs text-label-sm">
        <Check size={16} aria-hidden="true" />
        <span>已连接</span>
        <span className="font-mono text-body-sm text-mute">127.0.0.1:8787</span>
      </div>

      <dl className="m-0 mt-space-xl grid grid-cols-1 divide-y divide-hairline border-y border-hairline sm:grid-cols-3 sm:divide-x sm:divide-y-0">
        <Metric icon={<Server size={15} />} label="版本" value={`${data.name} v${data.version}`} />
        <Metric icon={<Cpu size={15} />} label="环境" value={`${data.goVersion} · ${data.platform}`} />
        <Metric icon={<Timer size={15} />} label="响应" value={`${latencyMs} ms`} />
      </dl>
    </div>
  )
}

function Metric({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
  return (
    <div className="flex min-w-0 items-center gap-space-sm px-space-md py-space-lg sm:px-space-lg">
      <span className="shrink-0 text-mute" aria-hidden="true">{icon}</span>
      <div className="min-w-0">
        <dt className="font-mono text-mono-eyebrow uppercase text-mute">{label}</dt>
        <dd className="mt-space-xxs truncate text-body-md text-ink">{value}</dd>
      </div>
    </div>
  )
}

function LoadingState() {
  return (
    <div className="flex min-h-56 items-center gap-space-sm pt-space-xl text-body-md text-mute" aria-live="polite">
      <RefreshCw className="animate-spin" size={16} aria-hidden="true" />
      <span>正在检测...</span>
    </div>
  )
}

function ErrorState({ message, isRefreshing, reload }: { message: string; isRefreshing: boolean; reload: () => void }) {
  const platform = detectPlatform()

  return (
    <div className="pt-space-xl" role="alert">
      <div className="flex items-start gap-space-sm">
        <WifiOff className="mt-0.5 shrink-0 text-error" size={17} aria-hidden="true" />
        <div>
          <h2 className="text-heading-md">Core 未运行</h2>
          <p className="mt-space-xs text-body-md text-body">{message}</p>
        </div>
      </div>

      <div className="mt-space-xl border-y border-hairline py-space-lg">
        <p className="text-body-md text-body">从 GitHub Release 下载并启动本地服务，然后回来重新检测。</p>
        <div className="mt-space-md flex flex-wrap items-center gap-space-sm">
          <Button asChild>
            <a href={releasesURL} target="_blank" rel="noreferrer">
              <Download aria-hidden="true" />
              下载 {platform.label}版本
            </a>
          </Button>
          <Button type="button" variant="outline" onClick={reload} disabled={isRefreshing}>
            <RefreshCw aria-hidden="true" className={isRefreshing ? 'animate-spin' : undefined} />
            重新检测
          </Button>
          <a className="inline-flex items-center gap-space-xxs text-body-sm text-link underline underline-offset-4" href="https://github.com/zhiyanzhaijie/warmmo/releases" target="_blank" rel="noreferrer">
            查看全部版本
            <ExternalLink size={13} aria-hidden="true" />
          </a>
        </div>
      </div>

      <div className="grid gap-space-lg pt-space-lg sm:grid-cols-2">
        <div>
          <div className="flex items-center gap-space-xs font-mono text-mono-eyebrow uppercase text-mute">
            <Terminal size={13} aria-hidden="true" />
            启动方式
          </div>
          <p className="mt-space-xs text-body-sm text-body">{platform.runHint}</p>
        </div>
        <div>
          <p className="font-mono text-mono-eyebrow uppercase text-mute">连接地址</p>
          <code className="mt-space-xs block text-body-sm text-ink">http://127.0.0.1:8787</code>
        </div>
      </div>
    </div>
  )
}

function detectPlatform() {
  const platform = navigator.userAgent.toLowerCase()
  if (platform.includes('mac')) {
    return { label: 'macOS', runHint: '解压后双击 start-warmmo-core.command。' }
  }
  if (platform.includes('win')) {
    return { label: 'Windows', runHint: '解压后双击 start-warmmo-core.cmd。' }
  }
  return { label: 'Linux', runHint: '解压后运行 ./start-warmmo-core.sh。' }
}
