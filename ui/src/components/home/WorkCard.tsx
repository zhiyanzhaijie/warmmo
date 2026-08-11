import { Archive, ArrowUpRight, LoaderCircle, MoreHorizontal, Pencil, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { useDeleteWork } from '../../apis/work-apis'
import type { WorkSummary } from '../../types/work'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../ui/alert-dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '../ui/dropdown-menu'
import { CanvasThumbnail } from './CanvasThumbnail'

interface WorkCardProps {
  work: WorkSummary
  onEdit?: () => void
  onArchive?: () => void
}

export function WorkCard({ work, onEdit, onArchive }: WorkCardProps) {
  const deleteWork = useDeleteWork()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const handleDelete = async () => {
    setDeleteError(null)
    try {
      await deleteWork.mutateAsync(work.id)
    } catch (error) {
      setDeleteError(error instanceof Error ? error.message : '删除作品失败')
    }
  }

  return (
    <>
      <article className="group relative overflow-hidden rounded-sm bg-canvas-elevated transition-colors hover:bg-hairline-soft">
        <Link className="block text-ink no-underline" to={`/works/${work.id}`}>
          <CanvasThumbnail nodes={work.previewNodes} edges={work.previewEdges} />
          <div className="p-space-md">
            <div className="flex items-start justify-between gap-space-sm">
              <div className="min-w-0">
                <h3 className="truncate pr-space-lg text-label-sm">{work.title}</h3>
                <p className="mt-space-xxs truncate pr-space-xl text-body-sm text-mute">
                  {work.folderName || '未分类'} · {work.nodeCount} 个节点 · {formatUpdatedAt(work.updatedAt)}
                </p>
              </div>
              {onEdit === undefined ? <ArrowUpRight className="shrink-0 text-faint transition-colors group-hover:text-ink" size={16} aria-hidden="true" /> : null}
            </div>
          </div>
        </Link>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="absolute right-space-xs bottom-space-sm grid size-8 cursor-pointer place-items-center rounded-sm text-mute opacity-100 transition-[opacity,background-color] hover:bg-canvas-elevated hover:text-ink data-[state=open]:opacity-100 sm:opacity-0 sm:group-hover:opacity-100" type="button" aria-label={`${work.title}的作品操作`} title="作品操作">
              <MoreHorizontal size={16} aria-hidden="true" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {onEdit !== undefined ? <DropdownMenuItem onSelect={onEdit}><Pencil /> 编辑信息</DropdownMenuItem> : null}
            {onArchive !== undefined ? <DropdownMenuItem onSelect={onArchive}><Archive /> {work.status === 'archived' ? '恢复作品' : '归档作品'}</DropdownMenuItem> : null}
            <DropdownMenuItem variant="destructive" onSelect={() => {
              setDeleteError(null)
              setDeleteDialogOpen(true)
            }}><Trash2 /> 删除作品</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </article>

      <AlertDialog open={deleteDialogOpen} onOpenChange={(open) => {
        if (!deleteWork.isPending) setDeleteDialogOpen(open)
      }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除“{work.title}”？</AlertDialogTitle>
            <AlertDialogDescription>
              画布中的节点、连线、版本历史、AI 运行记录和章节归档都会被永久删除，此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteError !== null ? <p className="mt-space-md text-body-sm text-error" role="alert">{deleteError}</p> : null}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteWork.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction disabled={deleteWork.isPending} onClick={(event) => {
              event.preventDefault()
              void handleDelete()
            }}>
              {deleteWork.isPending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : <Trash2 aria-hidden="true" />}
              {deleteWork.isPending ? '正在删除' : '确认删除'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function formatUpdatedAt(value: string) {
  const date = new Date(value)
  const seconds = Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000))
  if (seconds < 60) return '刚刚'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)} 小时前`
  if (seconds < 604_800) return `${Math.floor(seconds / 86_400)} 天前`
  return new Intl.DateTimeFormat('zh-CN', { month: 'short', day: 'numeric' }).format(date)
}
