import type { ChatStatus } from 'ai'
import { Paperclip } from 'lucide-react'
import { memo, useEffect, useState } from 'react'

import {
  Confirmation,
  ConfirmationAction,
  ConfirmationActions,
  ConfirmationRequest,
  ConfirmationTitle,
} from '@/components/ai-elements/confirmation'

import {
  PromptInput,
  PromptInputBody,
  PromptInputButton,
  PromptInputFooter,
  PromptInputHeader,
  type PromptInputMessage,
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from '@/components/ai-elements/prompt-input'
import { ModelSelector } from '@/components/models/ModelSelector'
import { TooltipProvider } from '@/components/ui/tooltip'
import { CanvasContextNodes } from '@/features/canvas/agent-workspace/ContextNodes'
import type { StoryFlowNode } from '@/features/canvas/flownode/types'
import type { EnabledModel } from '@/types/provider'

const preserveTextOnlyPaste = () => undefined

export interface PendingAgentInput {
  runId: string
  approvalEventId: string
  question: string
  options: string[]
  lastSequence: number
}

interface CanvasAgentPromptInputProps {
  canSubmit: boolean
  contextNodes: StoryFlowNode[]
  hasError: boolean
  isStreaming: boolean
  isSubmitting: boolean
  model: EnabledModel | null
  nodeKind: string
  pendingInput: PendingAgentInput | null
  prompt: string
  isResponding: boolean
  onModelChange: (model: EnabledModel | null) => void
  onPromptChange: (prompt: string) => void
  onRespond: (answer: string) => void
  onSubmit: (prompt: string) => void
}

export const CanvasAgentPromptInput = memo(function CanvasAgentPromptInput({
  canSubmit,
  contextNodes,
  hasError,
  isStreaming,
  isSubmitting,
  model,
  nodeKind,
  pendingInput,
  prompt,
  isResponding,
  onModelChange,
  onPromptChange,
  onRespond,
  onSubmit,
}: CanvasAgentPromptInputProps) {
  const [answer, setAnswer] = useState('')
  const [selectedOption, setSelectedOption] = useState('')
  useEffect(() => {
    setAnswer('')
    setSelectedOption('')
  }, [pendingInput?.approvalEventId])

  const isAnswerMode = pendingInput !== null
  const responseText = [selectedOption, answer.trim()].filter(Boolean).join('\n')
  const status: ChatStatus = isSubmitting || isResponding
    ? 'submitted'
    : isStreaming
      ? 'streaming'
      : hasError
        ? 'error'
        : 'ready'
  const submitPrompt = (message: PromptInputMessage) => {
    if (pendingInput !== null) {
      if (responseText === '' || isResponding) return
      onRespond(responseText)
      return
    }
    const nextPrompt = message.text.trim()
    if (!canSubmit || nextPrompt === '') return
    onSubmit(nextPrompt)
  }

  return (
    <TooltipProvider>
      <PromptInput
        className="[&_[data-slot=input-group]]:overflow-hidden [&_[data-slot=input-group]]:rounded-sm [&_[data-slot=input-group]]:border-0 [&_[data-slot=input-group]]:bg-canvas-elevated [&_[data-slot=input-group]]:shadow-none"
        data-node-kind={nodeKind}
        onSubmit={submitPrompt}
      >
        <PromptInputHeader className={isAnswerMode ? 'p-space-sm pb-0' : 'p-0'}>
          {pendingInput === null ? (
            <CanvasContextNodes nodes={contextNodes} />
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
          <PromptInputTextarea
            className="min-h-28 max-h-48 px-space-md py-space-sm text-body-md leading-6 text-ink placeholder:text-faint"
            onChange={(event) => isAnswerMode
              ? setAnswer(event.currentTarget.value)
              : onPromptChange(event.currentTarget.value)}
            onPaste={preserveTextOnlyPaste}
            placeholder={isAnswerMode ? '补充你的回答（可选）' : '告诉这个节点接下来要做什么'}
            value={isAnswerMode ? answer : prompt}
          />
        </PromptInputBody>

        <PromptInputFooter className="min-h-11 flex-wrap gap-space-xs px-space-sm pb-space-sm">
          {isAnswerMode ? (
            <span className="px-space-xs text-body-sm text-mute">等待你的决定</span>
          ) : <PromptInputTools>
            <PromptInputButton
              aria-label="附加素材"
              disabled
              tooltip="附件上传将在 Agent 接口支持后开放"
            >
              <Paperclip size={15} />
            </PromptInputButton>
            <ModelSelector
              capability="text"
              value={model}
              onValueChange={onModelChange}
              autoSelectFirst
              compact
              className="h-8 min-w-0 max-w-44 border-transparent bg-hairline-soft px-space-xs text-body-sm"
              ariaLabel="选择当前节点使用的文本模型"
            />
          </PromptInputTools>}
          <PromptInputSubmit
            aria-label={isAnswerMode ? '继续 Agent 执行' : status === 'submitted' || status === 'streaming' ? '节点指令运行中' : '运行节点指令'}
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
