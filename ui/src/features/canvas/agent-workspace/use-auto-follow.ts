import { useEffect, useState } from 'react'

const bottomThreshold = 48

export function useAutoFollow(enabled: boolean) {
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!enabled || viewport === null) return
    let frame: number | null = null
    let observedContent: Element | null = null
    let pinnedToBottom = true

    const atBottom = () => viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight <= bottomThreshold
    const scheduleFollow = () => {
      if (!pinnedToBottom || frame !== null) return
      frame = requestAnimationFrame(() => {
        frame = null
        viewport.scrollTop = viewport.scrollHeight
      })
    }
    const resizeObserver = new ResizeObserver(scheduleFollow)
    const observeContent = () => {
      const content = viewport.firstElementChild
      if (content === observedContent) return
      if (observedContent !== null) resizeObserver.unobserve(observedContent)
      observedContent = content
      if (observedContent !== null) resizeObserver.observe(observedContent)
    }
    const mutationObserver = new MutationObserver(() => {
      observeContent()
      scheduleFollow()
    })
    const handleScroll = () => {
      pinnedToBottom = atBottom()
    }

    observeContent()
    mutationObserver.observe(viewport, { childList: true, subtree: true })
    viewport.addEventListener('scroll', handleScroll, { passive: true })
    scheduleFollow()

    return () => {
      if (frame !== null) cancelAnimationFrame(frame)
      resizeObserver.disconnect()
      mutationObserver.disconnect()
      viewport.removeEventListener('scroll', handleScroll)
    }
  }, [enabled, viewport])

  return setViewport
}
