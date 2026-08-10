import { KeyRound, Languages, Palette } from 'lucide-react'
import type { ReactNode } from 'react'

import { AgentProviderSettings } from '../components/settings/AgentProviderSettings'

export function SettingsPage() {
  return (
    <main className="mx-auto max-w-app px-space-lg py-space-3xl">
      <div className="max-w-2xl">
        <p className="font-mono text-mono-eyebrow uppercase text-mute">Preferences</p>
        <h1 className="mt-space-xs text-heading-lg">设置</h1>
        <p className="mt-space-sm text-body-lg text-body">管理 Warmmo 的语言、主题和 Agent Provider。</p>
      </div>
      <div className="mt-space-2xl grid grid-cols-[13rem_minmax(0,1fr)] gap-space-2xl">
        <nav aria-label="设置分类" className="space-y-1">
          <SettingNavItem icon={<Languages size={16} />} label="语言" />
          <SettingNavItem icon={<Palette size={16} />} label="外观" />
          <SettingNavItem active icon={<KeyRound size={16} />} label="Agent Provider" />
        </nav>
        <AgentProviderSettings />
      </div>
    </main>
  )
}

function SettingNavItem({ icon, label, active = false }: { icon: ReactNode; label: string; active?: boolean }) {
  return (
    <button
      className={`flex h-10 w-full items-center gap-space-sm rounded-sm px-space-sm text-left text-button-md transition-colors ${active ? 'bg-hairline-soft text-ink' : 'text-body hover:bg-hairline-soft hover:text-ink'}`}
      type="button"
    >
      <span className={active ? 'text-ink' : 'text-mute'}>{icon}</span>
      {label}
    </button>
  )
}
