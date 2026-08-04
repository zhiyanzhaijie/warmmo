import { ArrowUp, FilePlus2, Sparkles } from 'lucide-react'
import { useCallback, useState, type FormEvent } from 'react'

import type { EnabledModel } from '../../types/provider'
import { ModelSelector } from '../models/ModelSelector'

interface PromptComposerProps {
  onCreate: (prompt: string, model: EnabledModel) => void
  onCreateBlank: () => void
}

export function PromptComposer({ onCreate, onCreateBlank }: PromptComposerProps) {
  const [prompt, setPrompt] = useState('')
  const [model, setModel] = useState<EnabledModel | null>(null)
  const canSubmit = prompt.trim().length > 0 && model !== null
  const selectModel = useCallback((nextModel: EnabledModel | null) => setModel(nextModel), [])

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const normalizedPrompt = prompt.trim()
    if (normalizedPrompt.length === 0 || model === null) {
      return
    }

    onCreate(normalizedPrompt, model)
    setPrompt('')
  }

  return (
    <section className="mx-auto max-w-[56rem] pt-space-4xl" aria-labelledby="home-hero-title">
      <div className="mb-space-xl flex flex-col items-start justify-between gap-space-lg sm:flex-row sm:items-end">
        <div className="max-w-[42rem]">
          <p className="flex items-center gap-space-xs font-mono text-mono-eyebrow uppercase text-mute">
            <Sparkles size={13} aria-hidden="true" /> Story canvas
          </p>
          <h1 id="home-hero-title" className="mt-space-sm text-[2.5rem] leading-[1.1] font-semibold text-ink sm:text-display-xl">
            今天，从哪段故事开始？
          </h1>
          <p className="mt-space-md max-w-[36rem] text-body-lg text-body">
            写下一段尚未成形的想法，让角色、事件与世界逐步展开。
          </p>
        </div>
        <button
          className="flex h-9 shrink-0 cursor-pointer items-center gap-space-xs rounded-sm px-space-sm text-button-md text-body transition-colors hover:bg-hairline-soft hover:text-ink"
          type="button"
          onClick={onCreateBlank}
        >
          <FilePlus2 size={15} aria-hidden="true" />
          <span>空白画布</span>
        </button>
      </div>

      <form className="overflow-hidden rounded-md bg-canvas-elevated shadow-floating ring-1 ring-ink/5" onSubmit={handleSubmit}>
        <label className="sr-only" htmlFor="story-prompt">小说创作描述</label>
        <textarea
          id="story-prompt"
          className="block min-h-44 w-full resize-none border-0 bg-transparent px-space-lg pt-space-lg text-body-lg text-ink outline-none placeholder:text-faint"
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
          placeholder="描述故事背景、主要角色或你想展开的情节..."
        />

        <div className="flex min-h-16 flex-col items-stretch justify-between gap-space-xs px-space-md pb-space-md sm:flex-row sm:items-center sm:gap-space-md">
          <ModelSelector
            capability="text"
            value={model}
            onValueChange={selectModel}
            autoSelectFirst
            compact
            className="h-9 w-full min-w-0 max-w-none border-0 bg-hairline-soft px-space-sm sm:w-auto sm:max-w-48"
            ariaLabel="选择文本模型"
          />

          <button
            className="flex h-10 shrink-0 items-center justify-center gap-space-xs rounded-sm bg-primary px-space-md text-button-md text-on-primary transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-30"
            type="submit"
            disabled={!canSubmit}
            title="创建工作"
          >
            <span>开始创作</span>
            <ArrowUp size={15} aria-hidden="true" />
          </button>
        </div>
      </form>
    </section>
  )
}
