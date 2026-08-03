import { useCallback, useState } from 'react'

import { PromptComposer } from '../components/home/PromptComposer'
import { RecentWorks } from '../components/home/RecentWorks'
import { recentWorks } from '../data/recentWorks'
import type { WorkSummary } from '../types/work'

const recentWorkLimit = 6

export function HomePage() {
  const [works, setWorks] = useState<WorkSummary[]>(recentWorks)
  const [creationNotice, setCreationNotice] = useState('')

  const createWork = useCallback((prompt: string, modelName: string) => {
    const title = prompt.length > 16 ? `${prompt.slice(0, 16)}...` : prompt
    const work: WorkSummary = {
      id: `draft-${Date.now()}`,
      title,
      updatedLabel: '刚刚',
      nodeCount: 1,
      modelName: modelName === 'gpt-4.1' ? 'GPT-4.1' : 'Claude Sonnet 4',
      status: 'initializing',
      previewNodes: [
        { id: 'idea', label: '故事概念', kind: 'plot', x: 36, y: 38 },
      ],
      previewEdges: [],
    }

    setWorks((currentWorks) => [work, ...currentWorks].slice(0, recentWorkLimit))
    setCreationNotice(`“${title}”已进入初始化队列`)
  }, [])

  const createBlankWork = useCallback(() => {
    const work: WorkSummary = {
      id: `blank-${Date.now()}`,
      title: '未命名小说',
      updatedLabel: '刚刚',
      nodeCount: 0,
      modelName: '尚未选择模型',
      status: 'draft',
      previewNodes: [],
      previewEdges: [],
    }

    setWorks((currentWorks) => [work, ...currentWorks].slice(0, recentWorkLimit))
    setCreationNotice('已创建空白工作')
  }, [])

  return (
    <div className="mx-auto max-w-app px-space-lg pb-space-3xl pt-space-3xl">
      <PromptComposer onCreate={createWork} onCreateBlank={createBlankWork} />
      <p className="mx-auto mt-space-sm min-h-5 max-w-[52.5rem] text-body-sm text-mute" role="status">{creationNotice}</p>
      <div className="mt-space-3xl">
        <RecentWorks works={works} onCreateBlank={createBlankWork} />
      </div>
    </div>
  )
}
