import { ArrowRight } from 'lucide-react'
import { Link } from 'react-router-dom'

import type { WorkSummary } from '../../types/work'
import { NewWorkCard } from './NewWorkCard'
import { WorkCard } from './WorkCard'

interface RecentWorksProps {
  works: WorkSummary[]
  onCreateBlank: () => void
}

export function RecentWorks({ works, onCreateBlank }: RecentWorksProps) {
  return (
    <section aria-labelledby="recent-works-title">
      <div className="mb-space-md flex items-center justify-between">
        <div>
          <p className="font-mono text-mono-eyebrow uppercase text-mute">Recent</p>
          <h2 id="recent-works-title" className="mt-space-xxs text-heading-md">最近工作</h2>
        </div>
        <Link className="flex items-center gap-space-xs text-button-md text-body no-underline hover:text-ink" to="/workspace">
          <span>全部工作</span>
          <ArrowRight size={15} aria-hidden="true" />
        </Link>
      </div>

      <div className="grid grid-cols-3 gap-space-md xl:grid-cols-4">
        <NewWorkCard onCreate={onCreateBlank} />
        {works.map((work) => <WorkCard key={work.id} work={work} />)}
      </div>
    </section>
  )
}
