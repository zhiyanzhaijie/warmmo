import type { CanvasNode, CanvasNodePosition } from '@/types/canvas'
import type { ChapterArchiveVisibility } from '@/types/chapter-archive'

export interface ArchivedNodeDragLayout {
  rootNodeId: string
  rootStartX: number
  rootStartY: number
  initialPositions: CanvasNodePosition[]
}

export function createArchivedNodeDragLayout(
  rootNodeId: string,
  nodes: CanvasNode[],
  archives: ChapterArchiveVisibility[],
): ArchivedNodeDragLayout | null {
  const rootNode = nodes.find((node) => node.id === rootNodeId)
  if (rootNode === undefined) return null
  const nodeIds = new Set<string>([rootNodeId])
  if (rootNode.kind === 'chapter-outline') {
    const archive = archives.find((candidate) => candidate.chapterOutlineNodeId === rootNodeId)
    if (archive === undefined) return null
    for (const section of archive.sections) {
      nodeIds.add(section.sectionOutlineNodeId)
      nodeIds.add(section.chapterSectionNodeId)
    }
  } else if (rootNode.kind === 'section-outline') {
    const archive = archives.find((candidate) =>
      candidate.sections.some((section) => section.sectionOutlineNodeId === rootNodeId))
    if (archive === undefined) return null
    const section = archive.sections.find((candidate) => candidate.sectionOutlineNodeId === rootNodeId)
    if (section !== undefined) nodeIds.add(section.chapterSectionNodeId)
  } else {
    return null
  }

  const positionByNodeId = new Map(nodes.map((node) => [node.id, node] as const))
  const initialPositions = [...nodeIds].flatMap((nodeId) => {
    const node = positionByNodeId.get(nodeId)
    return node === undefined ? [] : [{ nodeId, x: node.x, y: node.y }]
  })
  const root = initialPositions.find((position) => position.nodeId === rootNodeId)
  if (root === undefined || initialPositions.length !== nodeIds.size || initialPositions.length === 1) return null

  return {
    rootNodeId,
    rootStartX: root.x,
    rootStartY: root.y,
    initialPositions,
  }
}

export function translateArchivedNodeDragLayout(
  layout: ArchivedNodeDragLayout,
  rootX: number,
  rootY: number,
): CanvasNodePosition[] {
  const deltaX = rootX - layout.rootStartX
  const deltaY = rootY - layout.rootStartY
  return layout.initialPositions.map((position) => ({
    nodeId: position.nodeId,
    x: position.x + deltaX,
    y: position.y + deltaY,
  }))
}
