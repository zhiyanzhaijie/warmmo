import { useState, type ReactNode } from 'react'

import {
  createFlowNodeStore,
  FlowNodeStoreContext,
} from '@/features/canvas/flownode/store'

export function FlowNodeStoreProvider({ children }: { children: ReactNode }) {
  const [store] = useState(createFlowNodeStore)
  return <FlowNodeStoreContext.Provider value={store}>{children}</FlowNodeStoreContext.Provider>
}
