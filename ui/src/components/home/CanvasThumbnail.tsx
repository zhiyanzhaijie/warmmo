import type { PreviewNodeKind, WorkPreviewEdge, WorkPreviewNode } from '../../types/work'

interface CanvasThumbnailProps {
  nodes: WorkPreviewNode[]
  edges: WorkPreviewEdge[]
}

const nodeStyles: Record<PreviewNodeKind, string> = {
  chapter: 'border-link/30 bg-link-soft text-link-deep',
  character: 'border-hairline bg-canvas-elevated text-ink',
  plot: 'border-primary/20 bg-primary text-on-primary',
  world: 'border-hairline bg-hairline-soft text-body',
}

export function CanvasThumbnail({ nodes, edges }: CanvasThumbnailProps) {
  const nodeByID = new Map(nodes.map((node) => [node.id, node]))

  return (
    <div className="relative aspect-[16/10] overflow-hidden border-b border-hairline bg-[radial-gradient(circle,var(--color-hairline)_1px,transparent_1px)] bg-[size:16px_16px]">
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
              x1={`${source.x + 12}%`}
              y1={`${source.y + 8}%`}
              x2={`${target.x + 12}%`}
              y2={`${target.y + 8}%`}
              stroke="var(--color-hairline)"
              strokeWidth="1"
            />
          )
        })}
      </svg>

      {nodes.map((node) => (
        <span
          key={node.id}
          className={`absolute max-w-28 truncate rounded-sm border px-space-xs py-space-xxs text-body-sm shadow-whisper ${nodeStyles[node.kind]}`}
          style={{ left: `${node.x}%`, top: `${node.y}%` }}
        >
          {node.label}
        </span>
      ))}
    </div>
  )
}
