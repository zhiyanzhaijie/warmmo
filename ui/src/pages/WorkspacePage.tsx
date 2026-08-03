import { Plus } from 'lucide-react'

import { WorkCard } from '../components/home/WorkCard'
import { recentWorks } from '../data/recentWorks'

export function WorkspacePage() {
  return (
    <main className="mx-auto max-w-app px-space-lg py-space-3xl">
      <div className="flex items-end justify-between gap-space-lg">
        <div>
          <p className="font-mono text-mono-eyebrow uppercase text-mute">Workspace</p>
          <h1 className="mt-space-xs text-heading-lg">全部工作</h1>
          <p className="mt-space-sm text-body-lg text-body">按最近操作时间管理所有小说创作。</p>
        </div>
        <button className="flex h-10 cursor-pointer items-center gap-space-xs rounded-sm bg-primary px-space-sm text-button-md text-on-primary" type="button">
          <Plus size={15} aria-hidden="true" /> 新建工作
        </button>
      </div>

      <section className="mt-space-2xl" aria-label="工作列表">
        <div className="grid grid-cols-3 gap-space-md xl:grid-cols-4">
          {recentWorks.map((work) => <WorkCard key={work.id} work={work} />)}
        </div>
      </section>
    </main>
  )
}
