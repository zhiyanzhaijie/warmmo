import { Download, ExternalLink, RefreshCw } from 'lucide-react'

import { useRuntimeInfo } from '@/apis/runtime-apis'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'

import type { RuntimeInfo } from '../types/runtime'

const releasesURL = 'https://github.com/zhiyanzhaijie/warmmo/releases/latest'
const allReleasesURL = 'https://github.com/zhiyanzhaijie/warmmo/releases'
const coreAddress = '127.0.0.1:8787'

export function RuntimePage() {
  const { state, isRefreshing, reload } = useRuntimeInfo()

  return (
    <main className="bg-canvas text-ink">
      <section className="mx-auto flex min-h-[calc(100dvh-4rem)] w-full max-w-app flex-col px-space-lg py-space-3xl" aria-labelledby="runtime-title">
        {state.status === 'loading' ? <LoadingState /> : null}
        {state.status === 'error' ? <ErrorState message={state.message} isRefreshing={isRefreshing} reload={reload} /> : null}
        {state.status === 'success' ? <SuccessState data={state.data} latencyMs={state.latencyMs} /> : null}
      </section>
    </main>
  )
}

function StatusKicker({ connected }: { connected: boolean }) {
  return (
    <p className="flex items-center gap-space-xs text-label-sm text-ink">
      {connected ? <LiveDot /> : <span className="size-2 shrink-0 rounded-full bg-error" aria-hidden="true" />}
      {connected ? '已连接' : '未连接'}
      <span className="font-mono text-body-sm font-normal text-mute">· {coreAddress}</span>
    </p>
  )
}

function LiveDot() {
  return (
    <span className="relative flex size-2 shrink-0" aria-hidden="true">
      <span className="absolute inline-flex size-full animate-ping rounded-full bg-ink opacity-30" />
      <span className="relative inline-flex size-2 rounded-full bg-ink" />
    </span>
  )
}

function SuccessState({ data, latencyMs }: { data: RuntimeInfo; latencyMs: number }) {
  return (
    <>
      <div className="flex flex-1 flex-col justify-center py-space-4xl" aria-live="polite">
        <StatusKicker connected />
        <h1 id="runtime-title" className="mt-space-md max-w-2xl text-[2.5rem] leading-[1.1] font-semibold text-ink sm:text-display-xl">
          本地 Core 运行中
        </h1>
        <p className="mt-space-md max-w-xl text-body-lg text-body">创作与 AI 服务均已就绪，随时可以继续。</p>
      </div>

      <div className="grid grid-cols-1 gap-space-xs font-mono text-body-sm text-mute sm:grid-cols-3">
        <span>{data.name} v{data.version}</span>
        <span>{data.goVersion} · {data.platform}</span>
        <span>{latencyMs} ms</span>
      </div>
    </>
  )
}

function LoadingState() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-space-sm py-space-4xl" aria-live="polite">
      <h1 id="runtime-title" className="sr-only">本地 Core 检测中</h1>
      <Spinner className="text-mute" aria-hidden="true" />
      <p className="text-body-md text-mute">正在检测本地 Core...</p>
    </div>
  )
}

function ErrorState({ message, isRefreshing, reload }: { message: string; isRefreshing: boolean; reload: () => void }) {
  const platform = detectPlatform()

  return (
    <>
      <div className="flex flex-1 flex-col justify-center py-space-4xl" role="alert">
        <StatusKicker connected={false} />
        <h1 id="runtime-title" className="mt-space-md max-w-2xl text-[2.5rem] leading-[1.1] font-semibold text-ink sm:text-display-xl">
          本地 Core 未运行
        </h1>
        <p className="mt-space-md font-mono text-body-sm text-mute">{message}</p>
        <p className="mt-space-md max-w-xl text-body-lg text-body">从 GitHub Release 下载并启动本地服务（{platform.runHint}），完成后回到这里重新检测。</p>
        <div className="mt-space-xl flex flex-wrap items-center gap-space-sm">
          <Button asChild size="lg">
            <a href={releasesURL} target="_blank" rel="noreferrer">
              <Download aria-hidden="true" />
              下载 {platform.label} 版本
            </a>
          </Button>
          <Button type="button" variant="ghost" size="lg" onClick={reload} disabled={isRefreshing}>
            <RefreshCw aria-hidden="true" className={isRefreshing ? 'animate-spin' : undefined} />
            重新检测
          </Button>
          <a className="inline-flex items-center gap-space-xxs text-body-sm text-link underline underline-offset-4" href={allReleasesURL} target="_blank" rel="noreferrer">
            查看全部版本
            <ExternalLink size={13} aria-hidden="true" />
          </a>
        </div>
      </div>
    </>
  )
}

function detectPlatform() {
  const platform = navigator.userAgent.toLowerCase()
  if (platform.includes('mac')) {
    return { label: 'macOS', runHint: '解压后双击 start-warmmo-core.command' }
  }
  if (platform.includes('win')) {
    return { label: 'Windows', runHint: '解压后双击 start-warmmo-core.cmd' }
  }
  return { label: 'Linux', runHint: '解压后运行 ./start-warmmo-core.sh' }
}
