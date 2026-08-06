import { ReactFlowProvider } from '@xyflow/react'
import { useState } from 'react'
import { useParams } from 'react-router-dom'

import '@xyflow/react/dist/style.css'
import '@/features/canvas/canvas.css'

import { useCanvasCandidates, useCanvasEdges, useCanvasNodes } from '@/apis/canvas-apis'
import { CanvasAgentWorkspace } from '@/features/canvas/agent-workspace'
import { NodeDerivationToolbar } from '@/features/canvas/agent-workspace/NodeDerivationToolbar'
import { CanvasHeader } from '@/features/canvas/CanvasHeader'
import { FlowNodeStoreProvider } from '@/features/canvas/flownode/FlowNodeStoreProvider'
import { useSyncFlowNodes } from '@/features/canvas/flownode/use-sync-flow-nodes'
import { NodeDetailSheet } from '@/features/canvas/node-detail/NodeDetailSheet'
import { CanvasNodeCreator } from '@/features/canvas/node-creator'
import { CanvasSurface } from '@/features/canvas/surface'
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
  const nodesQuery = useCanvasNodes(workId)
  const candidatesQuery = useCanvasCandidates(workId)
  const edgesQuery = useCanvasEdges(workId)
  useSyncFlowNodes(nodesQuery.data, candidatesQuery.data, edgesQuery.data)

  return (
    <main className="relative h-dvh w-full overflow-hidden bg-canvas text-ink">
      <CanvasSurface workId={workId} />
      <CanvasHeader workId={workId} />
      <CanvasNodeCreator workId={workId} />
      <CanvasAgentWorkspace workId={workId} model={model} onModelChange={setModel} />
      <NodeDerivationToolbar workId={workId} model={model} />
      <NodeDetailSheet workId={workId} />
    </main>
  )
}
