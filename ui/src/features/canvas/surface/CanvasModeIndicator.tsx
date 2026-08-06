import { Panel } from '@xyflow/react'
import { Crosshair, Pencil, X } from 'lucide-react'
import { memo } from 'react'

import type { CanvasInteractionMode } from '@/features/canvas/flownode/store'

interface CanvasModeIndicatorProps {
  mode: CanvasInteractionMode
  onExitContextPicker: () => void
}

export const CanvasModeIndicator = memo(function CanvasModeIndicator({
  mode,
  onExitContextPicker,
}: CanvasModeIndicatorProps) {
  const isContextPicker = mode.kind === 'context-node-picker'

  return (
    <Panel className="warmnote-flow__mode-panel nodrag nopan nowheel" position="top-center">
      <div
        aria-live="polite"
        className={`warmnote-flow__mode-indicator ${isContextPicker ? 'warmnote-flow__mode-indicator--context-picker' : 'warmnote-flow__mode-indicator--editing'}`}
        role="status"
      >
        {isContextPicker ? <Crosshair aria-hidden="true" size={14} /> : <Pencil aria-hidden="true" size={14} />}
        <span>{isContextPicker ? '选择上下文' : '编辑模式'}</span>
        {isContextPicker ? (
          <button
            aria-label="退出上下文选择"
            className="warmnote-flow__mode-exit"
            title="退出上下文选择"
            type="button"
            onClick={onExitContextPicker}
          >
            <X aria-hidden="true" size={14} />
          </button>
        ) : null}
      </div>
    </Panel>
  )
})
