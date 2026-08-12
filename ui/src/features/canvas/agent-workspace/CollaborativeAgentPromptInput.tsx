import type { ChatStatus } from 'ai'
import { Compass, Feather } from 'lucide-react'
import { memo, useEffect, useRef, useState, type ChangeEvent } from 'react'

import type { CollaborativeAgentTarget } from '@/apis/canvas-apis'
import {
  Confirmation,
  ConfirmationAction,
  ConfirmationActions,
  ConfirmationRequest,
  ConfirmationTitle,
} from '@/components/ai-elements/confirmation'
import {
  Context,
  ContextCacheUsage,
  ContextContent,
  ContextContentBody,
  ContextContentFooter,
  ContextContentHeader,
  ContextInputUsage,
  ContextOutputUsage,
  ContextReasoningUsage,
  ContextTrigger,
} from '@/components/ai-elements/context'
import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputHeader,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from '@/components/ai-elements/prompt-input'
import { ModelSelector } from '@/components/models/ModelSelector'
import { Button } from '@/components/ui/button'
import { TooltipProvider } from '@/components/ui/tooltip'
import {
  CanvasPromptEditor,
  type CanvasPromptEditorHandle,
  type CanvasPromptValue,
} from '@/features/canvas/agent-workspace/CanvasPromptEditor'
import { CanvasContextNodes } from '@/features/canvas/agent-workspace/ContextNodes'
import type {
  CanvasAgentPromptSubmission,
  CanvasContextNode,
  PendingAgentInput,
} from '@/features/canvas/agent-workspace/types'
import type { EnabledModel } from '@/types/provider'
import type { AgentConversationUsage } from '@/types/canvas'

const modes: Array<{
  icon: typeof Feather
  label: string
  target: CollaborativeAgentTarget
}> = [
  { icon: Compass, label: '闲聊', target: 'collaborative-explore' },
  { icon: Feather, label: '创作', target: 'collaborative-targeted' },
]

interface CollaborativeAgentPromptInputProps {
  attachmentNodeIds: ReadonlySet<string>
  attachmentNodes: CanvasContextNode[]
  availableContextNodes: CanvasContextNode[]
  canSubmit: boolean
  contextUsage?: AgentConversationUsage
  contextWindowTokens?: number | null
  modelId?: string
  isContextPicking: boolean
  isResponding: boolean
  isStreaming: boolean
  isSubmitting: boolean
  mode: CollaborativeAgentTarget
  model: EnabledModel | null
  pendingInput: PendingAgentInput | null
  prompt: CanvasPromptValue
  onContextNodeAdd: (nodeId: string) => void
  onContextNodeRemove: (nodeId: string) => void
  onContextPickerToggle: () => void
  onModeChange: (mode: CollaborativeAgentTarget) => void
  onModelChange: (model: EnabledModel | null) => void
  onPromptChange: (prompt: CanvasPromptValue) => void
  onRespond: (answer: string) => void
  onSubmit: (input: CanvasAgentPromptSubmission) => void
}

export const CollaborativeAgentPromptInput = memo(function CollaborativeAgentPromptInput({
  attachmentNodeIds,
  attachmentNodes,
  availableContextNodes,
  canSubmit,
  contextUsage,
  contextWindowTokens,
  modelId,
  isContextPicking,
  isResponding,
  isStreaming,
  isSubmitting,
  mode,
  model,
  pendingInput,
  prompt,
  onContextNodeAdd,
  onContextNodeRemove,
  onContextPickerToggle,
  onModeChange,
  onModelChange,
  onPromptChange,
  onRespond,
  onSubmit,
}: CollaborativeAgentPromptInputProps) {
  const [answer, setAnswer] = useState('')
  const [selectedOption, setSelectedOption] = useState('')
  const promptDraftRef = useRef(prompt)
  const promptEditorRef = useRef<CanvasPromptEditorHandle>(null)
  const isAnswerMode = pendingInput !== null

  useEffect(() => {
    setAnswer('')
    setSelectedOption('')
  }, [pendingInput?.approvalEventId])

  useEffect(() => {
    for (const nodeId of promptDraftRef.current.contextNodeIds) {
      if (attachmentNodeIds.has(nodeId)) continue
      promptEditorRef.current?.removeContextNode(nodeId)
    }
  }, [attachmentNodeIds])

  const responseText = [selectedOption, answer.trim()].filter(Boolean).join('\n')
  const usedTokens = (contextUsage?.inputTokens ?? 0) + (contextUsage?.outputTokens ?? 0)
  const contextUsageDetails = {
    inputTokens: contextUsage?.inputTokens ?? 0,
    inputTokenDetails: {
      noCacheTokens: Math.max((contextUsage?.inputTokens ?? 0) - (contextUsage?.cachedInputTokens ?? 0), 0),
      cacheReadTokens: contextUsage?.cachedInputTokens ?? 0,
      cacheWriteTokens: 0,
    },
    outputTokens: contextUsage?.outputTokens ?? 0,
    outputTokenDetails: { textTokens: contextUsage?.outputTokens ?? 0, reasoningTokens: 0 },
    totalTokens: usedTokens,
  }
  const status: ChatStatus = isSubmitting || isResponding
    ? 'submitted'
    : isStreaming
      ? 'streaming'
      : 'ready'

  const submitPrompt = () => {
    if (pendingInput !== null) {
      if (responseText === '' || isResponding) return
      onRespond(responseText)
      return
    }
    const nextPrompt = promptDraftRef.current.requestText.trim()
    if (!canSubmit || nextPrompt === '') return
    onSubmit({ contextNodeIds: [...attachmentNodeIds], prompt: nextPrompt })
  }

  const handlePromptChange = (value: CanvasPromptValue) => {
    const previousContextNodeIds = new Set(promptDraftRef.current.contextNodeIds)
    promptDraftRef.current = value
    for (const nodeId of value.contextNodeIds) {
      if (!previousContextNodeIds.has(nodeId)) onContextNodeAdd(nodeId)
    }
    onPromptChange(value)
  }

  return (
    <TooltipProvider>
      <PromptInput
        className="border-0 outline-none [&_.ProseMirror]:!px-space-xs [&_.ProseMirror]:!pb-space-xs [&_[data-slot=prompt-placeholder]]:!left-space-xs [&_[data-slot=input-group]]:relative [&_[data-slot=input-group]]:min-h-0 [&_[data-slot=input-group]]:items-stretch [&_[data-slot=input-group]]:overflow-visible [&_[data-slot=input-group]]:rounded-sm [&_[data-slot=input-group]]:!border-0 [&_[data-slot=input-group]]:bg-canvas-elevated [&_[data-slot=input-group]]:!shadow-none [&_[data-slot=input-group]]:!ring-0 [&_[data-slot=input-group]]:!outline-none"
        data-node-kind="collaborative"
        onSubmit={submitPrompt}
      >
        <PromptInputHeader className={isAnswerMode ? 'p-space-sm pb-0' : 'p-0'}>
          {pendingInput === null ? (
            <div className="w-full [&>div]:px-space-xs [&>div]:pt-0">
              <CanvasContextNodes
                disabled={isSubmitting || isStreaming}
                isPicking={isContextPicking}
                nodes={attachmentNodes}
                onPickerToggle={onContextPickerToggle}
                onRemove={(nodeId) => {
                  promptEditorRef.current?.removeContextNode(nodeId)
                  onContextNodeRemove(nodeId)
                }}
              />
            </div>
          ) : (
            <Confirmation
              approval={{ id: pendingInput.approvalEventId }}
              state="approval-requested"
              className="gap-space-sm rounded-sm border-hairline bg-canvas-subtle p-space-sm"
            >
              <ConfirmationRequest>
                <ConfirmationTitle className="text-body-sm leading-5 text-ink">
                  {pendingInput.question}
                </ConfirmationTitle>
                {pendingInput.options.length > 0 ? (
                  <ConfirmationActions className="mt-space-sm flex-wrap justify-start self-stretch">
                    {pendingInput.options.map((option) => (
                      <ConfirmationAction
                        key={option}
                        aria-pressed={selectedOption === option}
                        className="h-auto min-h-8 whitespace-normal px-space-sm py-space-xxs text-left text-body-sm"
                        variant={selectedOption === option ? 'default' : 'outline'}
                        onClick={() => setSelectedOption((current) => current === option ? '' : option)}
                      >
                        {option}
                      </ConfirmationAction>
                    ))}
                  </ConfirmationActions>
                ) : null}
              </ConfirmationRequest>
            </Confirmation>
          )}
        </PromptInputHeader>

        <PromptInputBody>
          {isAnswerMode ? (
            <PromptInputTextarea
              className="min-h-28 max-h-48 px-space-xs py-space-xs text-body-md leading-6 text-ink placeholder:text-faint"
              onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setAnswer(event.currentTarget.value)}
              placeholder="补充你的回答（可选）"
              value={answer}
            />
          ) : (
            <CanvasPromptEditor
              ref={promptEditorRef}
              ariaLabel="给全局创作 Agent 的消息"
              availableContextNodes={availableContextNodes}
              disabled={isSubmitting || isStreaming}
              initialValue={prompt}
              placeholder={mode === 'collaborative-targeted' ? '描述你想完成的创作目标' : '聊聊剧情、关系或任何想法'}
              onChange={handlePromptChange}
            />
          )}
        </PromptInputBody>

        <PromptInputFooter className="min-h-11 flex-wrap gap-space-xs px-space-xs pb-0">
          {isAnswerMode ? (
            <span className="px-space-xs text-body-sm text-mute">等待你的决定</span>
          ) : (
            <PromptInputTools className="flex-wrap">
              <div className="flex h-8 shrink-0 rounded-sm bg-hairline-soft p-px" role="group" aria-label="协作模式">
                {modes.map((option) => {
                  const Icon = option.icon
                  const active = option.target === mode
                  return (
                    <Button
                      key={option.target}
                      aria-pressed={active}
                      className={active ? 'h-full bg-canvas-elevated text-ink shadow-whisper hover:bg-canvas-elevated' : 'h-full text-mute'}
                      disabled={isSubmitting || isStreaming}
                      size="xs"
                      variant="ghost"
                      onClick={() => onModeChange(option.target)}
                    >
                      <Icon aria-hidden="true" size={12} />
                      {option.label}
                    </Button>
                  )
                })}
              </div>
              <ModelSelector
                capability="text"
                value={model}
                onValueChange={onModelChange}
                autoSelectFirst
                compact
                className="h-8 min-w-0 max-w-44 border-transparent bg-hairline-soft px-space-xs text-body-sm"
                ariaLabel="选择全局 Agent 使用的文本模型"
              />
              <Context
                maxTokens={contextWindowTokens ?? Math.max(usedTokens, 1)}
                modelId={modelId}
                usage={contextUsageDetails}
                usedTokens={contextWindowTokens === null || contextWindowTokens === undefined ? 0 : usedTokens}
              >
                <ContextTrigger
                  aria-label="查看模型上下文用量"
                  className="size-8 shrink-0 p-0 text-mute hover:bg-hairline-soft hover:text-ink [&>span]:sr-only [&>svg]:size-4"
                />
                <ContextContent align="end" side="top" sideOffset={8}>
                  {contextWindowTokens === null || contextWindowTokens === undefined ? (
                    <ContextContentHeader>
                      <div className="flex items-center justify-between gap-space-sm text-body-sm">
                        <span className="text-ink">模型上下文</span>
                        <span className="font-mono text-mute">{formatTokens(usedTokens)} tokens</span>
                      </div>
                    </ContextContentHeader>
                  ) : <ContextContentHeader />}
                  <ContextContentBody>
                    <ContextInputUsage />
                    <ContextOutputUsage />
                    <ContextReasoningUsage />
                    <ContextCacheUsage />
                  </ContextContentBody>
                  <ContextContentFooter>
                    <span className="text-mute">上下文窗口</span>
                    <span>{contextWindowTokens === null || contextWindowTokens === undefined
                      ? '由 provider/model 决定'
                      : `${formatTokens(contextWindowTokens)} tokens`}</span>
                  </ContextContentFooter>
                </ContextContent>
              </Context>
            </PromptInputTools>
          )}
          <PromptInputSubmit
            aria-label={isAnswerMode ? '继续 Agent 执行' : status === 'submitted' || status === 'streaming' ? 'Agent 指令运行中' : '运行 Agent 指令'}
            className="bg-primary text-on-primary hover:opacity-85"
            disabled={isAnswerMode ? responseText === '' || isResponding : !canSubmit}
            size={isAnswerMode ? 'sm' : 'icon-sm'}
            status={status}
          >
            {isAnswerMode && !isResponding ? '继续' : undefined}
          </PromptInputSubmit>
        </PromptInputFooter>
      </PromptInput>
    </TooltipProvider>
  )
})

function formatTokens(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10_000 ? 0 : 1)}K`
  return String(value)
}
