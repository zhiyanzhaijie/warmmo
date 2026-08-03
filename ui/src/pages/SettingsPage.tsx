import { KeyRound, Languages, Palette } from 'lucide-react'
import type { ReactNode } from 'react'

export function SettingsPage() {
  return (
    <main className="mx-auto max-w-app px-space-lg py-space-3xl">
      <div className="max-w-2xl">
        <p className="font-mono text-mono-eyebrow uppercase text-mute">Preferences</p>
        <h1 className="mt-space-xs text-heading-lg">设置</h1>
        <p className="mt-space-sm text-body-lg text-body">管理 Warmnote 的语言、主题和 Agent Provider。</p>
      </div>
      <div className="mt-space-2xl grid max-w-4xl grid-cols-3 gap-space-md">
        <SettingSection icon={<Languages size={17} />} title="语言" value="简体中文" />
        <SettingSection icon={<Palette size={17} />} title="主题" value="跟随系统" />
        <SettingSection icon={<KeyRound size={17} />} title="Agent Provider" value="尚未配置" />
      </div>
    </main>
  )
}

function SettingSection({ icon, title, value }: { icon: ReactNode; title: string; value: string }) {
  return (
    <button className="flex min-h-32 cursor-pointer flex-col items-start justify-between rounded-md border border-hairline bg-canvas-elevated p-space-lg text-left shadow-whisper transition-shadow hover:shadow-floating" type="button">
      <span className="grid size-8 place-items-center rounded-sm bg-hairline-soft text-body">{icon}</span>
      <span>
        <span className="block text-label-sm text-ink">{title}</span>
        <span className="mt-space-xxs block text-body-sm text-mute">{value}</span>
      </span>
    </button>
  )
}
