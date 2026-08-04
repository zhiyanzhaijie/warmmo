import { Archive, ArrowUpRight, MoreHorizontal, Pencil } from 'lucide-react'
import { Link } from 'react-router-dom'

import type { WorkSummary } from '../../types/work'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '../ui/dropdown-menu'
import { CanvasThumbnail } from './CanvasThumbnail'

interface WorkCardProps {
  work: WorkSummary
  onEdit?: () => void
  onArchive?: () => void
}

export function WorkCard({ work, onEdit, onArchive }: WorkCardProps) {
  return (
    <article className="group relative overflow-hidden rounded-sm bg-canvas-elevated transition-colors hover:bg-hairline-soft">
      <Link className="block text-ink no-underline" to={`/works/${work.id}`}>
        <CanvasThumbnail nodes={work.previewNodes} edges={work.previewEdges} />
        <div className="p-space-md">
          <div className="flex items-start justify-between gap-space-sm">
            <div className="min-w-0">
              <h3 className="truncate pr-space-lg text-label-sm">{work.title}</h3>
              <p className="mt-space-xxs truncate text-body-sm text-mute">
                {work.folderName || '未分类'} · {work.nodeCount} 个节点 · {formatUpdatedAt(work.updatedAt)}
              </p>
            </div>
            {onEdit === undefined ? <ArrowUpRight className="shrink-0 text-faint transition-colors group-hover:text-ink" size={16} aria-hidden="true" /> : null}
          </div>
        </div>
      </Link>
      {onEdit !== undefined ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="absolute right-space-xs bottom-space-sm grid size-8 cursor-pointer place-items-center rounded-sm text-mute opacity-100 transition-[opacity,background-color] hover:bg-canvas-elevated hover:text-ink data-[state=open]:opacity-100 sm:opacity-0 sm:group-hover:opacity-100" type="button" title="作品操作">
              <MoreHorizontal size={16} aria-hidden="true" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={onEdit}><Pencil /> 编辑信息</DropdownMenuItem>
            {onArchive !== undefined ? <DropdownMenuItem onSelect={onArchive}><Archive /> {work.status === 'archived' ? '恢复作品' : '归档作品'}</DropdownMenuItem> : null}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </article>
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
