import { useOnViewportChange, useReactFlow } from '@xyflow/react'
import { useCallback, useEffect } from 'react'

import { getFlowNodeDetailLevel } from '@/features/canvas/flownode/render-policy'
import { useFlowNodeStoreApi } from '@/features/canvas/flownode/store'

export function useSyncFlowNodeRenderDetail() {
  const flow = useReactFlow()
  const store = useFlowNodeStoreApi()
  const syncZoom = useCallback((zoom: number) => {
    store.getState().actions.setDetailLevel(getFlowNodeDetailLevel(zoom))
  }, [store])

  const onChange = useCallback(({ zoom }: { zoom: number }) => {
    syncZoom(zoom)
  }, [syncZoom])

  useOnViewportChange({ onChange })

  useEffect(() => {
    syncZoom(flow.getZoom())
  }, [flow, syncZoom])
}
