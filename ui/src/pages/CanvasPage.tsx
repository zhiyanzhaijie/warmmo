import { ReactFlowProvider } from '@xyflow/react'
import { useCallback, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'

import '@xyflow/react/dist/style.css'
import '@/features/canvas/canvas.css'

import { useCanvasCandidates, useCanvasEdges, useCanvasNodes, useLayoutCanvasChapter } from '@/apis/canvas-apis'
import { useCurrentChapterArchives, useStorySpine } from '@/apis/chapter-archive-apis'
import { CanvasAgentWorkspace } from '@/features/canvas/agent-workspace'
import { CollaborativeAgentDrawer } from '@/features/canvas/agent-workspace/CollaborativeAgentDrawer'
import { NodeDerivationToolbar } from '@/features/canvas/agent-workspace/NodeDerivationToolbar'
import { CanvasHeader } from '@/features/canvas/CanvasHeader'
import { FlowNodeStoreProvider } from '@/features/canvas/flownode/FlowNodeStoreProvider'
import { useSyncFlowNodes } from '@/features/canvas/flownode/use-sync-flow-nodes'
import { NodeDetailSheet } from '@/features/canvas/node-detail/NodeDetailSheet'
import { CanvasNodeCreator } from '@/features/canvas/node-creator'
import { CanvasSurface } from '@/features/canvas/surface'
import { StorySpineTimeline } from '@/features/canvas/story-spine/StorySpineTimeline'
import {
  collectArchiveLockedNodeIds,
  collectCollapsedArchiveGraph,
} from '@/features/canvas/story-spine/archive-visibility'
import type { EnabledModel } from '@/types/provider'

export function CanvasPage() {
  const { workId = 'local-work' } = useParams()

  return (
    <ReactFlowProvider>
      <FlowNodeStoreProvider key={workId}>
        <CanvasWorkspace workId={workId} />
      </FlowNodeStoreProvider>
    </ReactFlowProvider>
  )
}

function CanvasWorkspace({ workId }: { workId: string }) {
  const [model, setModel] = useState<EnabledModel | null>(null)
  const [expandedChapterNodeIds, setExpandedChapterNodeIds] = useState<ReadonlySet<string>>(() => new Set())
  const nodesQuery = useCanvasNodes(workId)
  const candidatesQuery = useCanvasCandidates(workId)
  const edgesQuery = useCanvasEdges(workId)
  const archivesQuery = useCurrentChapterArchives(workId)
  const storySpineQuery = useStorySpine(workId)
  const {
    isPending: isLayoutPending,
    mutate: layoutChapter,
    variables: layoutPendingChapterNodeId,
  } = useLayoutCanvasChapter(workId)
  const collapsedArchiveGraph = useMemo(
    () => collectCollapsedArchiveGraph(archivesQuery.data ?? [], expandedChapterNodeIds),
    [archivesQuery.data, expandedChapterNodeIds],
  )
  const archiveLockedNodeIds = useMemo(
    () => collectArchiveLockedNodeIds(archivesQuery.data ?? []),
    [archivesQuery.data],
  )
  const toggleChapter = useCallback((chapterNodeId: string) => {
    setExpandedChapterNodeIds((current) => {
      const next = new Set(current)
      if (next.has(chapterNodeId)) next.delete(chapterNodeId)
      else next.add(chapterNodeId)
      return next
    })
  }, [])
  const expandChapter = useCallback((chapterNodeId: string) => {
    setExpandedChapterNodeIds((current) => {
      if (current.has(chapterNodeId)) return current
      const next = new Set(current)
      next.add(chapterNodeId)
      return next
    })
  }, [])
  const layoutArchivedChapter = useCallback((chapterNodeId: string) => {
    layoutChapter(chapterNodeId)
  }, [layoutChapter])
  const archiveVisibilityReady = !archivesQuery.isPending
  const archiveOptions = useMemo(() => ({
    stateResolved: archivesQuery.isSuccess,
    lockedNodeIds: archiveLockedNodeIds,
    expandedChapterNodeIds,
    layoutPending: isLayoutPending,
    layoutPendingChapterNodeId: isLayoutPending ? layoutPendingChapterNodeId ?? null : null,
    onToggleChapter: toggleChapter,
    onLayoutChapter: layoutArchivedChapter,
  }), [
    archiveLockedNodeIds,
    archivesQuery.isSuccess,
    expandedChapterNodeIds,
    isLayoutPending,
    layoutArchivedChapter,
    layoutPendingChapterNodeId,
    toggleChapter,
  ])
  useSyncFlowNodes(
    archiveVisibilityReady ? nodesQuery.data : undefined,
    archiveVisibilityReady ? candidatesQuery.data : undefined,
    archiveVisibilityReady ? edgesQuery.data : undefined,
    collapsedArchiveGraph.hiddenNodeIds,
    collapsedArchiveGraph.proxyNodeIds,
    archiveOptions,
  )

  return (
    <main className="relative h-dvh w-full overflow-hidden bg-canvas text-ink">
      <CanvasSurface
        workId={workId}
        canvasNodes={nodesQuery.data ?? []}
        chapterArchives={archivesQuery.data ?? []}
      />
      <StorySpineTimeline
        workId={workId}
        archives={storySpineQuery.data?.items ?? []}
        canvasNodes={nodesQuery.data ?? []}
        expandedChapterNodeIds={expandedChapterNodeIds}
        onExpandChapter={expandChapter}
      />
      <CanvasHeader workId={workId} />
      <CanvasNodeCreator workId={workId} />
      <CanvasAgentWorkspace
        canvasEdges={edgesQuery.data ?? []}
        canvasNodes={nodesQuery.data ?? []}
        workId={workId}
        model={model}
        onModelChange={setModel}
      />
      <CollaborativeAgentDrawer
        canvasNodes={nodesQuery.data ?? []}
        model={model}
        workId={workId}
        onModelChange={setModel}
      />
      <NodeDerivationToolbar workId={workId} model={model} />
      <NodeDetailSheet workId={workId} />
    </main>
  )
}
