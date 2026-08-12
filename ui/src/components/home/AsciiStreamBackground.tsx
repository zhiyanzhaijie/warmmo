import { useEffect, useRef } from 'react'

import { useAsciiSource } from './ascii-source'

const FONT_SIZE = 18
const LINE_HEIGHT = 22
const SCROLL_SPEED = 18
const TYPE_RATIO = 0.8
const MAX_ALPHA = 0.8
const MAX_DPR = 3
const PAPER_TRANSFORM = 'perspective(1500px) rotateX(60deg) rotateY(0deg) rotateZ(0deg) scale(1)'

export function AsciiStreamBackground() {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const source = useAsciiSource()

  useEffect(() => {
    const canvas = canvasRef.current
    const ctx = canvas?.getContext('2d')
    if (canvas == null || ctx == null) return undefined

    let rafId = 0
    let width = 0
    let height = 0
    let lineCapacity = 0
    let lines: string[] = []
    let script: string[] = []
    let scriptIndex = 0
    let scrollOffset = 0
    let typeProgress = 0
    let charsPerSec = 24
    let lastTime: number | null = null
    let ink = '161, 161, 161'

    const motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')

    const wrapScript = () => {
      const limit = width * 0.96
      const wrapped: string[] = []
      let line = ''
      for (const ch of source) {
        if (ch === '\n') {
          if (line.trim() !== '') wrapped.push(line)
          line = ''
          continue
        }
        if (line !== '' && ctx.measureText(line + ch).width > limit) {
          wrapped.push(line)
          line = ch === ' ' ? '' : ch
        } else {
          line += ch
        }
      }
      if (line.trim() !== '') wrapped.push(line)
      return wrapped
    }

    const nextLine = () => {
      if (script.length === 0) return ''
      const line = script[scriptIndex % script.length]
      scriptIndex += 1
      return line
    }

    const resetTyping = (line: string) => {
      charsPerSec = (Math.max(1, line.length) * SCROLL_SPEED / LINE_HEIGHT) * TYPE_RATIO
      typeProgress = 0
    }

    const resolveInk = () => {
      const raw = getComputedStyle(document.documentElement).getPropertyValue('--color-faint').trim()
      const match = /^#([\da-f]{6})$/i.exec(raw)
      if (match === null) return
      const value = Number.parseInt(match[1], 16)
      ink = `${(value >> 16) & 255}, ${(value >> 8) & 255}, ${value & 255}`
    }

    const drawLine = (text: string, y: number) => {
      if (text === '' || y < -LINE_HEIGHT || y > height) return
      const depth = Math.min(1, Math.max(0, y / height))
      ctx.fillStyle = `rgba(${ink}, ${(MAX_ALPHA * depth ** 1.6).toFixed(3)})`
      ctx.fillText(text, 0, y)
    }

    const drawStatic = () => {
      ctx.clearRect(0, 0, width, height)
      for (let i = 0; i < lineCapacity; i += 1) {
        drawLine(nextLine(), height - LINE_HEIGHT - i * LINE_HEIGHT)
      }
    }

    const layout = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, MAX_DPR)
      width = canvas.clientWidth
      height = canvas.clientHeight
      canvas.width = Math.round(width * dpr)
      canvas.height = Math.round(height * dpr)
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.font = `${FONT_SIZE}px "Geist Mono Variable", "Geist Mono", monospace`
      ctx.textBaseline = 'top'
      script = wrapScript()
      scriptIndex = 0
      lineCapacity = Math.ceil(height / LINE_HEIGHT) + 2
      lines = Array.from({ length: lineCapacity }, nextLine)
      scrollOffset = 0
      typeProgress = lines[lines.length - 1]?.length ?? 0
      if (motionQuery.matches) drawStatic()
    }

    const frame = (now: number) => {
      rafId = window.requestAnimationFrame(frame)
      if (lastTime === null) {
        lastTime = now
        return
      }
      const dt = Math.min(0.1, (now - lastTime) / 1000)
      lastTime = now

      scrollOffset += SCROLL_SPEED * dt
      typeProgress += charsPerSec * dt

      if (scrollOffset >= LINE_HEIGHT) {
        scrollOffset -= LINE_HEIGHT
        const line = nextLine()
        lines.push(line)
        resetTyping(line)
        if (lines.length > lineCapacity) lines.splice(0, lines.length - lineCapacity)
      }

      ctx.font = `${FONT_SIZE}px "Geist Mono Variable", "Geist Mono", monospace`
      ctx.clearRect(0, 0, width, height)
      for (let i = 0; i < lines.length; i += 1) {
        const y = height - LINE_HEIGHT - (lines.length - 1 - i) * LINE_HEIGHT - scrollOffset
        const isTyping = i === lines.length - 1 && typeProgress < lines[i].length
        drawLine(isTyping ? lines[i].slice(0, Math.floor(typeProgress)) : lines[i], y)
      }
    }

    const handleMotionChange = () => {
      window.cancelAnimationFrame(rafId)
      lastTime = null
      if (motionQuery.matches) {
        drawStatic()
      } else {
        rafId = window.requestAnimationFrame(frame)
      }
    }

    const themeObserver = new MutationObserver(() => {
      resolveInk()
      if (motionQuery.matches) drawStatic()
    })

    resolveInk()
    layout()
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    motionQuery.addEventListener('change', handleMotionChange)
    window.addEventListener('resize', layout)
    if (!motionQuery.matches) rafId = window.requestAnimationFrame(frame)

    return () => {
      window.cancelAnimationFrame(rafId)
      themeObserver.disconnect()
      motionQuery.removeEventListener('change', handleMotionChange)
      window.removeEventListener('resize', layout)
    }
  }, [source])

  return (
    <div aria-hidden="true" className="pointer-events-none fixed inset-x-0 top-0 z-0 h-screen overflow-hidden">
      <div className="absolute inset-[-20%]" style={{ transform: PAPER_TRANSFORM }}>
        <canvas ref={canvasRef} className="size-full" />
      </div>
    </div>
  )
}
