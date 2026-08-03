import { useParams } from 'react-router-dom'

export function CanvasPage() {
  const { workId } = useParams()

  return (
    <main className="grid min-h-dvh place-items-center bg-[radial-gradient(circle,var(--color-hairline)_1px,transparent_1px)] bg-[size:20px_20px] text-body-md text-mute">
      <span className="font-mono text-mono-eyebrow uppercase">Canvas · {workId}</span>
    </main>
  )
}
