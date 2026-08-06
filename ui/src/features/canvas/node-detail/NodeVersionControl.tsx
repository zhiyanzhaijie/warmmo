import { History, LoaderCircle } from 'lucide-react'
import { memo } from 'react'

import { useCanvasNodeVersions, useSwitchCanvasNodeVersion } from '@/apis/canvas-apis'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { CanvasNode } from '@/types/canvas'

const versionDateFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

interface NodeVersionControlProps {
  workId: string
  node: CanvasNode
}

export const NodeVersionControl = memo(function NodeVersionControl({
  workId,
  node,
}: NodeVersionControlProps) {
  const versionsQuery = useCanvasNodeVersions(workId, node.id)
  const switchVersion = useSwitchCanvasNodeVersion(workId, node.id)
  const versions = versionsQuery.data ?? []

  if (versionsQuery.isPending) {
    return <LoaderCircle aria-label="正在读取节点版本" className="animate-spin text-mute" size={15} />
  }
  if (versions.length === 0) return null

  return (
    <div className="flex min-w-0 items-center gap-space-xs">
      <History aria-hidden="true" className="shrink-0 text-mute" size={15} />
      <Select
        value={node.currentVersionId || undefined}
        disabled={switchVersion.isPending}
        onValueChange={(versionId) => {
          if (versionId !== node.currentVersionId) switchVersion.mutate(versionId)
        }}
      >
        <SelectTrigger aria-label="切换节点版本" className="h-9 w-36 bg-canvas-elevated text-body-sm sm:w-44">
          <SelectValue placeholder="选择版本" />
        </SelectTrigger>
        <SelectContent align="end">
          {versions.map((version) => (
            <SelectItem key={version.id} value={version.id}>
              版本 {version.versionNumber} · {versionDateFormatter.format(new Date(version.createdAt))}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {switchVersion.isPending ? <LoaderCircle aria-hidden="true" className="animate-spin text-mute" size={14} /> : null}
      {switchVersion.isError ? (
        <span className="sr-only" role="alert">节点版本切换失败</span>
      ) : null}
    </div>
  )
})
