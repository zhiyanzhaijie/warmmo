import {
  nodeDefinitions,
  type CanvasNodeCategory,
} from '@/features/canvas/nodes/definitions'
import type { WorkPreviewEdge, WorkPreviewNode } from '@/types/work'

interface CanvasThumbnailProps {
  nodes: WorkPreviewNode[]
  edges: WorkPreviewEdge[]
}

const nodeStyles: Record<CanvasNodeCategory, string> = {
  entity: 'border-hairline bg-canvas-elevated text-ink',
  structure: 'border-primary/20 bg-primary text-on-primary',
  asset: 'border-link/30 bg-link-soft text-link-deep',
}

export function CanvasThumbnail({ nodes, edges }: CanvasThumbnailProps) {
  const nodeByID = new Map(nodes.map((node) => [node.id, node]))

  const xValues = nodes.map((node) => node.x)
  const yValues = nodes.map((node) => node.y)
  const minX = Math.min(...xValues)
  const maxX = Math.max(...xValues)
  const minY = Math.min(...yValues)
  const maxY = Math.max(...yValues)
  const projectX = (x: number) => nodes.length <= 1 ? 38 : 8 + ((x - minX) / Math.max(maxX - minX, 1)) * 66
  const projectY = (y: number) => nodes.length <= 1 ? 40 : 12 + ((y - minY) / Math.max(maxY - minY, 1)) * 58

  return (
    <div className="relative aspect-[16/10] overflow-hidden bg-[radial-gradient(circle,var(--color-hairline)_1px,transparent_1px)] bg-[size:16px_16px]">
      <svg className="absolute inset-0 size-full" aria-hidden="true">
        {edges.map((edge) => {
          const source = nodeByID.get(edge.source)
          const target = nodeByID.get(edge.target)
          if (source === undefined || target === undefined) {
            return null
          }

          return (
            <line
              key={`${edge.source}-${edge.target}`}
              x1={`${projectX(source.x) + 10}%`}
              y1={`${projectY(source.y) + 6}%`}
              x2={`${projectX(target.x) + 10}%`}
              y2={`${projectY(target.y) + 6}%`}
              stroke="var(--color-hairline)"
              strokeWidth="1"
            />
          )
        })}
      </svg>

      {nodes.map((node) => {
        const category = nodeDefinitions[node.kind]?.category ?? 'entity'
        return (
          <span
            key={node.id}
            className={`absolute max-w-28 truncate rounded-sm border px-space-xs py-space-xxs text-body-sm shadow-whisper ${nodeStyles[category]}`}
            style={{ left: `${projectX(node.x)}%`, top: `${projectY(node.y)}%` }}
          >
            {node.label}
          </span>
        )
      })}
    </div>
  )
}
