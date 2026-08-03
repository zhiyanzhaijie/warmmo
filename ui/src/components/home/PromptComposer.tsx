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
    <div className="mx-auto max-w-[52.5rem]">
      <div className="mb-space-lg flex items-center gap-space-sm">
        <span className="grid size-9 place-items-center rounded-full border border-hairline bg-canvas-elevated shadow-whisper">
          <Sparkles size={16} aria-hidden="true" />
        </span>
        <h1 className="text-heading-lg text-ink">今天，从哪段故事开始？</h1>
      </div>

      <form className="overflow-hidden rounded-lg border border-hairline bg-canvas-elevated shadow-floating" onSubmit={handleSubmit}>
        <label className="sr-only" htmlFor="story-prompt">小说创作描述</label>
        <textarea
          id="story-prompt"
          className="block min-h-32 w-full resize-none border-0 bg-transparent px-space-lg pt-space-lg text-body-lg text-ink outline-none placeholder:text-faint"
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
          placeholder="描述故事背景、主要角色或你想展开的情节..."
        />

        <div className="flex min-h-14 items-center justify-between gap-space-md border-t border-hairline px-space-sm">
          <div className="flex items-center gap-space-xs">
            <ModelSelector
              capability="text"
              value={model}
              onValueChange={selectModel}
              autoSelectFirst
              className="h-9"
              ariaLabel="选择文本模型"
            />
            <button
              className="flex h-9 cursor-pointer items-center gap-space-xs rounded-sm px-space-sm text-button-md text-body transition-colors hover:bg-hairline-soft hover:text-ink"
              type="button"
              onClick={onCreateBlank}
            >
              <FilePlus2 size={15} aria-hidden="true" />
              <span>空白画布</span>
            </button>
          </div>

          <button
            className="grid size-10 shrink-0 place-items-center rounded-full bg-primary text-on-primary transition-opacity disabled:cursor-not-allowed disabled:opacity-30"
            type="submit"
            disabled={!canSubmit}
            title="创建工作"
          >
            <ArrowUp size={17} aria-hidden="true" />
            <span className="sr-only">创建工作</span>
          </button>
        </div>
      </form>
    </div>
  )
}
