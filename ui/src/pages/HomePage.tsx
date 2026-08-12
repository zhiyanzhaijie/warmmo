import { useCallback, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { useInitialPromptStore } from '@/features/canvas/agent-workspace/initial-prompt-store'

import { useCreateWork, useWorks } from '../apis/work-apis'
import { AsciiStreamBackground } from '../components/home/AsciiStreamBackground'
import { PromptComposer } from '../components/home/PromptComposer'
import { RecentWorks } from '../components/home/RecentWorks'
import { WorkEditorDialog } from '../components/work/WorkEditorDialog'

const recentWorkLimit = 6

export function HomePage() {
  const navigate = useNavigate()
  const works = useWorks()
  const { mutate: createWork } = useCreateWork()
  const [creationNotice, setCreationNotice] = useState('')
  const [blankEditorOpen, setBlankEditorOpen] = useState(false)

  const createFromPrompt = useCallback((prompt: string) => {
    const title = prompt.length > 16 ? `${prompt.slice(0, 16)}...` : prompt
    createWork({ title, description: prompt, folderId: '' }, {
      onSuccess: (work) => {
        useInitialPromptStore.getState().setInitialPrompt(work.id, prompt)
        navigate(`/works/${work.id}`)
      },
      onError: () => setCreationNotice('创建失败，请确认 Warmmo Core 正在运行。'),
    })
  }, [createWork, navigate])

  const createBlankWork = useCallback(() => {
    setBlankEditorOpen(true)
  }, [])


  return (
    <div className="relative">
      <AsciiStreamBackground />
      <div className="relative z-10 mx-auto min-h-[calc(100dvh-4rem)] max-w-app px-space-lg pb-space-3xl">
        <PromptComposer onCreate={createFromPrompt} />
        <p className="mx-auto mt-space-sm min-h-5 max-w-[56rem] text-body-sm text-mute" role="status">{creationNotice}</p>
        <div className="mt-space-3xl">
          <RecentWorks works={(works.data ?? []).filter((work) => work.status === 'active').slice(0, recentWorkLimit)} onCreateBlank={createBlankWork} />
        </div>
      </div>
      <WorkEditorDialog open={blankEditorOpen} onOpenChange={setBlankEditorOpen} onSaved={(work) => navigate(`/works/${work.id}`)} />
    </div>
  )
}
