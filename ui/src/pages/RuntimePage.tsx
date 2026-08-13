import { Check, Copy, Download, ExternalLink, RefreshCw, Terminal } from 'lucide-react'
import { useState } from 'react'
import type { ReactNode } from 'react'

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
    <div className="grid flex-1 grid-cols-1 items-center gap-space-3xl py-space-3xl lg:grid-cols-[minmax(0,1fr)_minmax(20rem,28rem)] lg:gap-space-4xl" role="alert">
      <div className="flex flex-col justify-center">
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

      <PlatformRunGuide platform={platform} />
    </div>
  )
}

const macOSSteps = [
  {
    title: '进入解压目录',
    command: 'cd "$HOME/Downloads/warmmo-core_VERSION_darwin_arm64"',
  },
  {
    title: '移除隔离属性',
    command: 'xattr -dr com.apple.quarantine .',
  },
  {
    title: '启动本地 Core',
    command: './start-warmmo-core.command',
  },
]

function PlatformRunGuide({ platform }: { platform: ReturnType<typeof detectPlatform> }) {
  if (platform.label === 'macOS') return <MacOSRunGuide />
  if (platform.label === 'Windows') return <WindowsRunGuide />
  return <LinuxRunGuide />
}

function RunGuideFrame({ eyebrow, title, children }: { eyebrow: string; title: string; children: ReactNode }) {
  return (
    <aside className="pt-space-lg lg:pt-0" aria-labelledby="runtime-guide-title">
      <div className="flex items-center gap-space-xs font-mono text-mono-eyebrow uppercase text-mute">
        <span className="flex size-6 items-center justify-center rounded-full bg-link-soft text-ink" aria-hidden="true">
          <Terminal size={13} />
        </span>
        {eyebrow}
      </div>
      <h2 id="runtime-guide-title" className="mt-space-md text-heading-md text-ink">{title}</h2>
      {children}
    </aside>
  )
}

function MacOSRunGuide() {
  return (
    <RunGuideFrame eyebrow="macOS / First run" title="首次运行被系统阻止？">
      <p className="mt-space-xs text-body-md text-body">
        当前 Release 尚未经过 Apple 公证，下载后的文件可能带有 quarantine 属性。打开“终端”，按顺序执行以下命令即可放行。
      </p>
      <p className="mt-space-md flex items-start gap-space-xs text-body-sm text-mute">
        <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-warning" aria-hidden="true" />
        请先将第一步中的目录名替换为实际下载版本；Intel Mac 将末尾的 arm64 改为 amd64。
      </p>

      <ol className="relative mt-space-xl space-y-space-lg before:absolute before:top-3 before:bottom-3 before:left-[0.6875rem] before:w-px before:bg-hairline-soft">
        {macOSSteps.map((step, index) => (
          <li key={step.title} className="relative pl-space-xl">
            <span className="absolute top-0.5 left-0 flex size-[1.375rem] items-center justify-center rounded-full bg-canvas text-mono-eyebrow text-mute ring-1 ring-hairline-soft">
              {String(index + 1).padStart(2, '0')}
            </span>
            <p className="mb-space-xs text-label-sm text-ink">
              {step.title}
            </p>
            <BashBlock command={step.command} />
          </li>
        ))}
      </ol>
    </RunGuideFrame>
  )
}

function LinuxRunGuide() {
  return (
    <RunGuideFrame eyebrow="Linux / First run" title="从终端启动 Core">
      <p className="mt-space-xs text-body-md text-body">解压 Release 后进入目录，赋予脚本执行权限并启动本地服务。</p>
      <ol className="mt-space-xl space-y-space-lg before:absolute before:top-3 before:bottom-3 before:left-[0.6875rem] before:w-px before:bg-hairline-soft">
        <li>
          <p className="mb-space-xs text-label-sm text-ink"><span className="mr-space-xs font-mono text-body-sm text-mute">01</span>进入解压目录</p>
          <BashBlock command='cd "$HOME/Downloads/warmmo-core_VERSION_linux_amd64"' />
        </li>
        <li>
          <p className="mb-space-xs text-label-sm text-ink"><span className="mr-space-xs font-mono text-body-sm text-mute">02</span>启动本地 Core</p>
          <BashBlock command="./start-warmmo-core.sh" />
        </li>
      </ol>
    </RunGuideFrame>
  )
}

function WindowsRunGuide() {
  return (
    <RunGuideFrame eyebrow="Windows / First run" title="启动本地 Core">
      <p className="mt-space-xs text-body-md text-body">解压 ZIP 后双击根目录中的启动脚本。若 Windows Defender 弹出提示，请选择“更多信息”后继续运行。</p>
      <div className="mt-space-lg">
        <p className="mb-space-xs text-label-sm text-ink"><span className="mr-space-xs font-mono text-body-sm text-mute">01</span>PowerShell</p>
        <BashBlock command='.\\start-warmmo-core.cmd' />
      </div>
    </RunGuideFrame>
  )
}

function BashBlock({ command }: { command: string }) {
  const [copied, setCopied] = useState(false)

  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="group relative overflow-hidden border-b border-hairline-soft bg-canvas-elevated/70">
      <div className="border-b border-hairline-soft/70 px-space-sm py-space-xxs font-mono text-mono-eyebrow text-mute">bash</div>
      <pre className="overflow-x-auto px-space-sm py-space-sm pr-12 font-mono text-code text-ink"><code>{command}</code></pre>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        className="absolute top-8 right-space-xs text-mute hover:text-ink"
        onClick={() => void copyCommand()}
        aria-label={copied ? '已复制命令' : '复制命令'}
        title={copied ? '已复制' : '复制'}
      >
        {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      </Button>
    </div>
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
