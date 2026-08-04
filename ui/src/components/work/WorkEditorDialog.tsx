import { FolderPlus, LoaderCircle, Plus } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'

import { useCreateWork, useCreateWorkFolder, useUpdateWork, useWorkFolders } from '@/apis/work-apis'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { WorkDetail, WorkSummary } from '@/types/work'

const noFolderValue = '__none__'

interface WorkEditorDialogProps {
  open: boolean
  work?: WorkDetail | WorkSummary
  onOpenChange: (open: boolean) => void
  onSaved?: (work: WorkDetail | WorkSummary) => void
}

export function WorkEditorDialog({ open, work, onOpenChange, onSaved }: WorkEditorDialogProps) {
  const folders = useWorkFolders(open)
  const createWork = useCreateWork()
  const updateWork = useUpdateWork()
  const createFolder = useCreateWorkFolder()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [folderId, setFolderId] = useState('')
  const [creatingFolder, setCreatingFolder] = useState(false)
  const [folderName, setFolderName] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const editing = work !== undefined
  const pending = createWork.isPending || updateWork.isPending

  useEffect(() => {
    if (!open) return
    setTitle(work?.title ?? '')
    setDescription(work?.description ?? '')
    setFolderId(work?.folderId ?? '')
    setCreatingFolder(false)
    setFolderName('')
    setFormError(null)
  }, [open, work])

  const handleCreateFolder = async () => {
    const name = folderName.trim()
    if (name.length === 0) return
    setFormError(null)
    try {
      const folder = await createFolder.mutateAsync(name)
      setFolderId(folder.id)
      setFolderName('')
      setCreatingFolder(false)
    } catch (error) {
      setFormError(error instanceof Error ? error.message : '创建分类失败')
    }
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const normalizedTitle = title.trim()
    if (normalizedTitle.length === 0) {
      setFormError('请输入作品名称')
      return
    }
    setFormError(null)
    try {
      const saved = work === undefined
        ? await createWork.mutateAsync({ title: normalizedTitle, description: description.trim(), folderId })
        : await updateWork.mutateAsync({
            id: work.id,
            title: normalizedTitle,
            description: description.trim(),
            folderId,
            status: work.status,
            expectedRevision: work.revision,
          })
      onSaved?.(saved)
      onOpenChange(false)
    } catch (error) {
      setFormError(error instanceof Error ? error.message : '保存作品失败')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editing ? '编辑作品' : '新建作品'}</DialogTitle>
          <DialogDescription>{editing ? '修改名称、简介或所属分类。' : '先建立作品身份，画布内容可以稍后逐步补充。'}</DialogDescription>
        </DialogHeader>

        <form onSubmit={(event) => void handleSubmit(event)}>
          <div className="mt-space-lg space-y-space-md">
            <div>
              <Label htmlFor="work-title">作品名称</Label>
              <Input id="work-title" className="mt-space-xs" value={title} onChange={(event) => setTitle(event.target.value)} maxLength={120} autoFocus placeholder="例如：雾港来信" />
            </div>

            <div>
              <Label htmlFor="work-description">简介</Label>
              <textarea
                id="work-description"
                className="mt-space-xs block min-h-24 w-full resize-none rounded-sm bg-hairline-soft px-space-sm py-space-xs text-body-md text-ink outline-none transition-shadow placeholder:text-faint focus:ring-2 focus:ring-link-soft"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                maxLength={500}
                placeholder="用一两句话记录这个作品的方向"
              />
            </div>

            <div>
              <div className="flex items-center justify-between gap-space-sm">
                <Label>分类</Label>
                <button className="flex h-7 cursor-pointer items-center gap-space-xxs rounded-sm px-space-xs text-body-sm text-mute hover:bg-hairline-soft hover:text-ink" type="button" onClick={() => setCreatingFolder((value) => !value)}>
                  <FolderPlus size={13} aria-hidden="true" /> 新建分类
                </button>
              </div>
              {creatingFolder ? (
                <div className="mt-space-xs flex gap-space-xs">
                  <Input value={folderName} onChange={(event) => setFolderName(event.target.value)} maxLength={60} placeholder="分类名称" />
                  <button className="grid size-10 shrink-0 cursor-pointer place-items-center rounded-sm bg-primary text-on-primary disabled:cursor-not-allowed disabled:opacity-40" type="button" disabled={folderName.trim().length === 0 || createFolder.isPending} onClick={() => void handleCreateFolder()} title="创建分类">
                    {createFolder.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Plus size={15} />}
                  </button>
                </div>
              ) : (
                <Select value={folderId || noFolderValue} onValueChange={(value) => setFolderId(value === noFolderValue ? '' : value)}>
                  <SelectTrigger className="mt-space-xs border-0 bg-hairline-soft"><SelectValue placeholder="未分类" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value={noFolderValue}>未分类</SelectItem>
                    {(folders.data ?? []).map((folder) => <SelectItem key={folder.id} value={folder.id}>{folder.name}</SelectItem>)}
                  </SelectContent>
                </Select>
              )}
            </div>
          </div>

          {formError !== null ? <p className="mt-space-md text-body-sm text-error" role="alert">{formError}</p> : null}

          <DialogFooter>
            <button className="h-9 cursor-pointer rounded-sm px-space-sm text-button-md text-body hover:bg-hairline-soft hover:text-ink" type="button" onClick={() => onOpenChange(false)}>取消</button>
            <button className="flex h-9 cursor-pointer items-center gap-space-xs rounded-sm bg-primary px-space-md text-button-md text-on-primary disabled:cursor-wait disabled:opacity-50" type="submit" disabled={pending || title.trim().length === 0}>
              {pending ? <LoaderCircle className="animate-spin" size={14} aria-hidden="true" /> : null}
              {editing ? '保存' : '创建并进入画布'}
            </button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
