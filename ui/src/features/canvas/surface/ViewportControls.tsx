import { MiniMap, useReactFlow, useStore } from '@xyflow/react'
import { Focus, Map as MapIcon, ZoomIn, ZoomOut } from 'lucide-react'
import { memo, useCallback, useEffect, useRef, useState } from 'react'

import type { CanvasFlowEdge } from '@/features/canvas/flowedge/types'
import type { CanvasFlowNode } from '@/features/canvas/flownode/types'
import { isTextEntryTarget } from '@/features/canvas/keyboard'
import { canvasFitViewOptions, canvasMaxZoom, canvasMinZoom } from '@/features/canvas/surface/config'

const minimapNodeLimit = 2_000
const minimapStyle = {
  width: 144,
  height: 96,
  left: 14,
  bottom: 58,
  margin: 0,
}

export const CanvasViewportControls = memo(function CanvasViewportControls({ nodeCount }: { nodeCount: number }) {
  const flow = useReactFlow<CanvasFlowNode, CanvasFlowEdge>()
  const fittedInitialNodesRef = useRef(false)
  const [isMinimapVisible, setIsMinimapVisible] = useState(true)
  const isMinimapAvailable = nodeCount <= minimapNodeLimit

  const toggleMinimap = useCallback(() => {
    if (isMinimapAvailable) setIsMinimapVisible((visible) => !visible)
  }, [isMinimapAvailable])

  const zoomIn = useCallback(() => {
    void flow.zoomIn({ duration: 160 })
  }, [flow])

  const zoomOut = useCallback(() => {
    void flow.zoomOut({ duration: 160 })
  }, [flow])

  const fitCanvas = useCallback(() => {
    void flow.fitView({ ...canvasFitViewOptions, duration: 200 })
  }, [flow])

  useEffect(() => {
    if (fittedInitialNodesRef.current || nodeCount === 0) return
    fittedInitialNodesRef.current = true
    requestAnimationFrame(() => void flow.fitView(canvasFitViewOptions))
  }, [flow, nodeCount])

  useEffect(() => {
    const handleViewportShortcut = (event: KeyboardEvent) => {
      if (event.repeat || event.altKey || isTextEntryTarget(event.target)) return
      if (!event.ctrlKey && !event.metaKey) return

      if (event.code === 'Equal' || event.code === 'NumpadAdd') {
        event.preventDefault()
        zoomIn()
        return
      }
      if (event.code === 'Minus' || event.code === 'NumpadSubtract') {
        event.preventDefault()
        zoomOut()
        return
      }
      if (event.code === 'KeyF') {
        event.preventDefault()
        fitCanvas()
        return
      }
      if (event.code === 'KeyM' && isMinimapAvailable) {
        event.preventDefault()
        toggleMinimap()
      }
    }

    window.addEventListener('keydown', handleViewportShortcut)
    return () => window.removeEventListener('keydown', handleViewportShortcut)
  }, [fitCanvas, isMinimapAvailable, toggleMinimap, zoomIn, zoomOut])

  return (
    <>
      <CanvasViewportToolbar
        isMinimapAvailable={isMinimapAvailable}
        isMinimapVisible={isMinimapVisible}
        onFitCanvas={fitCanvas}
        onToggleMinimap={toggleMinimap}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
      />
      {isMinimapAvailable && isMinimapVisible ? (
        <MiniMap<CanvasFlowNode>
          ariaLabel="画布缩略图"
          bgColor="color-mix(in srgb, var(--color-ink) 9%, var(--color-canvas))"
          className="warmnote-flow__minimap"
          maskColor="color-mix(in srgb, var(--color-canvas) 76%, transparent)"
          nodeBorderRadius={2}
          nodeClassName={getMinimapNodeClassName}
          nodeColor="color-mix(in srgb, var(--color-ink) 58%, var(--color-canvas))"
          nodeStrokeColor="transparent"
          position="bottom-left"
          pannable
          style={minimapStyle}
          zoomable
        />
      ) : null}
    </>
  )
})

const CanvasViewportToolbar = memo(function CanvasViewportToolbar({
  isMinimapAvailable,
  isMinimapVisible,
  onFitCanvas,
  onToggleMinimap,
  onZoomIn,
  onZoomOut,
}: {
  isMinimapAvailable: boolean
  isMinimapVisible: boolean
  onFitCanvas: () => void
  onToggleMinimap: () => void
  onZoomIn: () => void
  onZoomOut: () => void
}) {
  const zoom = useStore((state) => state.transform[2])

  return (
    <div aria-label="画布视图控制" className="warmnote-flow__viewport-toolbar nodrag nopan nowheel" role="toolbar">
      <button
        aria-label="放大"
        className="warmnote-flow__viewport-button"
        disabled={zoom >= canvasMaxZoom}
        title="放大 · Ctrl/⌘ +"
        type="button"
        onClick={onZoomIn}
      >
        <ZoomIn size={15} aria-hidden="true" />
      </button>
      <button
        aria-label="缩小"
        className="warmnote-flow__viewport-button"
        disabled={zoom <= canvasMinZoom}
        title="缩小 · Ctrl/⌘ -"
        type="button"
        onClick={onZoomOut}
      >
        <ZoomOut size={15} aria-hidden="true" />
      </button>
      <button
        aria-label="适应画布"
        className="warmnote-flow__viewport-button"
        title="适应画布 · Ctrl/⌘ F"
        type="button"
        onClick={onFitCanvas}
      >
        <Focus size={15} aria-hidden="true" />
      </button>
      <button
        aria-label={isMinimapVisible ? '关闭小地图' : '打开小地图'}
        aria-pressed={isMinimapVisible && isMinimapAvailable}
        className={`warmnote-flow__viewport-button ${isMinimapVisible && isMinimapAvailable ? 'warmnote-flow__viewport-button--active' : ''}`}
        disabled={!isMinimapAvailable}
        title={isMinimapAvailable ? '切换小地图 · Ctrl/⌘ M' : '节点过多，无法显示小地图'}
        type="button"
        onClick={onToggleMinimap}
      >
        <MapIcon size={15} aria-hidden="true" />
      </button>
    </div>
  )
})

function getMinimapNodeClassName(node: CanvasFlowNode) {
  return node.type === 'selection-proxy' ? 'warmnote-flow__minimap-selection-proxy' : ''
}
