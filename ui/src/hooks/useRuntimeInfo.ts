import { useCallback, useEffect, useState } from 'react'

import { fetchRuntimeInfo } from '../services/runtimeService'
import type { RuntimeRequestState } from '../types/runtime'

export function useRuntimeInfo() {
  const [state, setState] = useState<RuntimeRequestState>({ status: 'loading' })

  const load = useCallback(async (signal?: AbortSignal) => {
    setState({ status: 'loading' })
    const startedAt = performance.now()

    try {
      const data = await fetchRuntimeInfo(signal)
      setState({
        status: 'success',
        data,
        latencyMs: Math.round(performance.now() - startedAt),
      })
    } catch (error) {
      if (signal?.aborted === true) {
        return
      }
      setState({
        status: 'error',
        message: error instanceof Error ? error.message : '未知连接错误',
      })
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  return { state, reload: () => void load() }
}
