import { ArrowUp, Sparkles } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'

import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from '@/components/ai-elements/prompt-input'
import type { EnabledModel } from '../../types/provider'
import { ModelSelector } from '../models/ModelSelector'

interface PromptComposerProps {
  onCreate: (prompt: string, model: EnabledModel) => void
}

const randomPoems = [
  "呜呼！何时眼前突兀现此屋...",
  "未经审视的生活是不值得过的",
  "多年以后，面对行刑队，奥雷里亚诺·布恩迪亚上校将会回想起..."
]

function useTypingPoem() {
  const [text, setText] = useState('')

  useEffect(() => {
    const pickPoem = (except = '') => {
      const candidates = randomPoems.filter((poem) => poem !== except)
      return candidates[Math.floor(Math.random() * candidates.length)]
    }
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setText(pickPoem())
      return undefined
    }

    let poem = pickPoem()
    let index = 0
    let deleting = false
    let timer = 0

    const tick = () => {
      if (!deleting) {
        index += 1
        setText(poem.slice(0, index))
        if (index >= poem.length) {
          deleting = true
          timer = window.setTimeout(tick, 2800)
          return
        }
        timer = window.setTimeout(tick, 100 + Math.random() * 80)
        return
      }
      index -= 1
      setText(poem.slice(0, index))
      if (index <= 0) {
        poem = pickPoem(poem)
        deleting = false
        timer = window.setTimeout(tick, 600)
        return
      }
      timer = window.setTimeout(tick, 26)
    }

    timer = window.setTimeout(tick, 900)
    return () => window.clearTimeout(timer)
  }, [])

  return text
}

export function PromptComposer({ onCreate }: PromptComposerProps) {
  const [prompt, setPrompt] = useState('')
  const [model, setModel] = useState<EnabledModel | null>(null)
  const canSubmit = prompt.trim().length > 0 && model !== null
  const selectModel = useCallback((nextModel: EnabledModel | null) => setModel(nextModel), [])
  const poem = useTypingPoem()

  function handleSubmit() {
    const normalizedPrompt = prompt.trim()
    if (normalizedPrompt === '' || model === null) {
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
            <Sparkles size={13} aria-hidden="true" /> 有想法，别错过!
          </p>
          <h1 id="home-hero-title" className="mt-space-sm text-[2.5rem] leading-[1.1] font-semibold text-ink sm:text-display-xl">
            今天，从哪段故事开始？
          </h1>
          <p aria-hidden="true" className="mt-space-md max-w-[36rem] text-body-lg text-body">
            {poem}
            <span className="ml-0.5 inline-block h-[1.1em] w-px translate-y-[0.2em] animate-pulse bg-body" />
          </p>
        </div>
      </div>

      <PromptInput
        className="[&_[data-slot=input-group]]:min-h-0 [&_[data-slot=input-group]]:items-stretch [&_[data-slot=input-group]]:rounded-md [&_[data-slot=input-group]]:border-0 [&_[data-slot=input-group]]:bg-canvas-elevated [&_[data-slot=input-group]]:shadow-floating [&_[data-slot=input-group]]:ring-1 [&_[data-slot=input-group]]:ring-ink/5"
        onSubmit={handleSubmit}
      >
        <PromptInputBody>
          <PromptInputTextarea
            aria-label="小说创作描述"
            className="max-h-72 min-h-36 px-space-lg pt-space-lg text-body-lg text-ink placeholder:text-faint"
            onChange={(event) => setPrompt(event.currentTarget.value)}
            placeholder="描述故事背景、主要角色或你想展开的情节..."
            value={prompt}
          />
        </PromptInputBody>
        <PromptInputFooter className="min-h-16 flex-wrap gap-space-xs px-space-md pb-space-md">
          <PromptInputTools>
            <ModelSelector
              capability="text"
              value={model}
              onValueChange={selectModel}
              autoSelectFirst
              compact
              className="h-9 w-full min-w-0 max-w-none border-transparent bg-hairline-soft px-space-sm sm:w-auto sm:max-w-48"
              ariaLabel="选择文本模型"
            />
          </PromptInputTools>
          <PromptInputSubmit
            aria-label="创建工作"
            className="h-10 gap-space-xs rounded-sm bg-primary px-space-md text-on-primary hover:opacity-90 disabled:opacity-30"
            disabled={!canSubmit}
            size="sm"
            title="创建工作"
          >
            <span>开始创作</span>
            <ArrowUp aria-hidden="true" className="size-4" />
          </PromptInputSubmit>
        </PromptInputFooter>
      </PromptInput>
    </section>
  )
}
