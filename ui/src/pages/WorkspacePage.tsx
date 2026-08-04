import { Archive, Folder, LoaderCircle, Plus, RotateCcw } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'

import { useUpdateWork, useWorkFolders, useWorks } from '../apis/work-apis'
import { WorkCard } from '../components/home/WorkCard'
import { WorkEditorDialog } from '../components/work/WorkEditorDialog'
import type { WorkSummary } from '../types/work'

type WorkFilter = 'all' | 'uncategorized' | 'archived' | string

type EditorState =
  | { mode: 'create' }
  | { mode: 'edit'; work: WorkSummary }
  | null

export function WorkspacePage() {
  const navigate = useNavigate()
  const works = useWorks()
  const folders = useWorkFolders()
  const updateWork = useUpdateWork()
  const [filter, setFilter] = useState<WorkFilter>('all')
  const [editor, setEditor] = useState<EditorState>(null)
  const visibleWorks = useMemo(() => (works.data ?? []).filter((work) => {
    if (filter === 'archived') return work.status === 'archived'
    if (work.status !== 'active') return false
    if (filter === 'all') return true
    if (filter === 'uncategorized') return work.folderId === ''
    return work.folderId === filter
  }), [filter, works.data])

  const toggleArchive = (work: WorkSummary) => {
    updateWork.mutate({
      id: work.id,
      title: work.title,
      description: work.description,
      folderId: work.folderId,
      status: work.status === 'archived' ? 'active' : 'archived',
      expectedRevision: work.revision,
    })
  }

  return (
    <main className="mx-auto min-h-[calc(100vh-64px)] max-w-app px-space-lg py-space-3xl">
      <div className="flex flex-col items-start justify-between gap-space-lg sm:flex-row sm:items-end">
        <div>
          <p className="font-mono text-mono-eyebrow uppercase text-mute">Workspace</p>
          <h1 className="mt-space-xs text-heading-lg">全部工作</h1>
          <p className="mt-space-sm text-body-lg text-body">{works.data?.length ?? 0} 个作品，按最近操作时间排列。</p>
        </div>
        <button
          className="flex h-10 cursor-pointer items-center gap-space-xs rounded-sm bg-primary px-space-md text-button-md text-on-primary transition-opacity hover:opacity-90 disabled:cursor-wait disabled:opacity-60"
          type="button"
          onClick={() => setEditor({ mode: 'create' })}
        >
          <Plus size={15} aria-hidden="true" />
          新建工作
        </button>
      </div>

      <section className="mt-space-3xl border-t border-hairline pt-space-lg" aria-label="工作列表">
        {works.isSuccess ? (
          <div className="mb-space-xl flex flex-wrap items-center gap-space-xs" aria-label="作品分类">
            <FilterButton active={filter === 'all'} onClick={() => setFilter('all')}>全部</FilterButton>
            <FilterButton active={filter === 'uncategorized'} onClick={() => setFilter('uncategorized')}>未分类</FilterButton>
            {(folders.data ?? []).map((folder) => (
              <FilterButton key={folder.id} active={filter === folder.id} onClick={() => setFilter(folder.id)}>
                <Folder size={13} aria-hidden="true" /> {folder.name}
              </FilterButton>
            ))}
            <FilterButton active={filter === 'archived'} onClick={() => setFilter('archived')}>
              <Archive size={13} aria-hidden="true" /> 已归档
            </FilterButton>
          </div>
        ) : null}
        {works.isPending ? <WorkspaceLoading /> : null}
        {works.isError ? <WorkspaceError onRetry={() => void works.refetch()} /> : null}
        {works.isSuccess && visibleWorks.length === 0 ? <WorkspaceEmpty onCreate={() => setEditor({ mode: 'create' })} filtered={works.data.length > 0} /> : null}
        {works.isSuccess && visibleWorks.length > 0 ? (
          <div className="grid grid-cols-1 gap-x-space-lg gap-y-space-xl sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {visibleWorks.map((work) => (
              <WorkCard key={work.id} work={work} onEdit={() => setEditor({ mode: 'edit', work })} onArchive={() => toggleArchive(work)} />
            ))}
          </div>
        ) : null}
      </section>

      {updateWork.isError ? <p className="mt-space-md text-body-sm text-error">更新作品失败，请重新打开后再操作。</p> : null}

      <WorkEditorDialog
        open={editor !== null}
        work={editor?.mode === 'edit' ? editor.work : undefined}
        onOpenChange={(open) => { if (!open) setEditor(null) }}
        onSaved={(work) => {
          if (editor?.mode === 'create') navigate(`/works/${work.id}`)
        }}
      />
    </main>
  )
}

function FilterButton({ active, children, onClick }: { active: boolean; children: ReactNode; onClick: () => void }) {
  return (
    <button className={`flex h-8 cursor-pointer items-center gap-space-xxs rounded-sm px-space-sm text-body-sm transition-colors ${active ? 'bg-primary text-on-primary' : 'text-body hover:bg-hairline-soft hover:text-ink'}`} type="button" onClick={onClick}>
      {children}
    </button>
  )
}

function WorkspaceLoading() {
  return (
    <div className="flex min-h-64 items-center justify-center text-mute" role="status">
      <LoaderCircle className="animate-spin" size={20} aria-hidden="true" />
      <span className="sr-only">正在加载工作列表</span>
    </div>
  )
}

function WorkspaceError({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="flex min-h-64 flex-col items-center justify-center text-center">
      <p className="text-label-sm text-ink">暂时无法读取工作列表</p>
      <p className="mt-space-xs text-body-sm text-mute">请确认 Warmnote Core 正在运行。</p>
      <button className="mt-space-md flex h-9 cursor-pointer items-center gap-space-xs rounded-sm bg-hairline-soft px-space-sm text-button-md text-ink hover:bg-hairline" type="button" onClick={onRetry}>
        <RotateCcw size={14} aria-hidden="true" /> 重试
      </button>
    </div>
  )
}

function WorkspaceEmpty({ onCreate, filtered }: { onCreate: () => void; filtered: boolean }) {
  return (
    <div className="flex min-h-72 flex-col items-center justify-center text-center">
      <p className="font-mono text-mono-eyebrow uppercase text-mute">{filtered ? 'No matching works' : 'No works yet'}</p>
      <h2 className="mt-space-xs text-heading-md">{filtered ? '这个分类还是空的' : '从一张空画布开始'}</h2>
      <p className="mt-space-xs max-w-sm text-body-md text-body">{filtered ? '可以编辑作品信息，把它移动到当前分类。' : '角色、设定、事件和章节会在这里形成你的作品结构。'}</p>
      {!filtered ? (
        <button className="mt-space-lg flex h-10 cursor-pointer items-center gap-space-xs rounded-sm bg-primary px-space-md text-button-md text-on-primary hover:opacity-90" type="button" onClick={onCreate}>
          <Plus size={15} aria-hidden="true" /> 新建工作
        </button>
      ) : null}
    </div>
  )
}
