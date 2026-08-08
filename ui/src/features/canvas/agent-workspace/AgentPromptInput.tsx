import type { ChatStatus } from 'ai'
import { Paperclip } from 'lucide-react'
import { memo, useEffect, useRef, useState, type ChangeEvent } from 'react'

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
  PromptInputSubmit,
  PromptInputTextarea,
  PromptInputTools,
} from '@/components/ai-elements/prompt-input'
import { ModelSelector } from '@/components/models/ModelSelector'
import { TooltipProvider } from '@/components/ui/tooltip'
import {
  CanvasPromptEditor,
  type CanvasPromptEditorHandle,
  type CanvasPromptValue,
} from '@/features/canvas/agent-workspace/CanvasPromptEditor'
import { CanvasContextNodes } from '@/features/canvas/agent-workspace/ContextNodes'
import type { CanvasContextNode } from '@/features/canvas/agent-workspace/types'
import type { EnabledModel } from '@/types/provider'
import { cn } from '@/lib/utils'

const preserveTextOnlyPaste = () => undefined

export interface PendingAgentInput {
  runId: string
  approvalEventId: string
  question: string
  options: string[]
  lastSequence: number
}

export interface CanvasAgentPromptSubmission {
  contextNodeIds: string[]
  prompt: string
}

interface CanvasAgentPromptInputProps {
  ariaLabel?: string
  attachmentNodeIds: ReadonlySet<string>
  attachmentNodes: CanvasContextNode[]
  availableContextNodes: CanvasContextNode[]
  canSubmit: boolean
  hasError: boolean
  isContextPicking: boolean
  isStreaming: boolean
  isSubmitting: boolean
  model: EnabledModel | null
  nodeKind: string
  pendingAttachmentNodeIds: ReadonlySet<string>
  pendingInput: PendingAgentInput | null
  prompt: CanvasPromptValue
  className?: string
  placeholder?: string
  showContextPicker?: boolean
  isResponding: boolean
  onContextNodeRemove: (nodeId: string) => void
  onContextPickerToggle: () => void
  onModelChange: (model: EnabledModel | null) => void
  onPromptChange: (prompt: CanvasPromptValue) => void
  onPriorityContextNodeAdd: (nodeId: string) => void
  onRespond: (answer: string) => void
  onSubmit: (input: CanvasAgentPromptSubmission) => void
}

export const CanvasAgentPromptInput = memo(function CanvasAgentPromptInput({
  attachmentNodeIds,
  ariaLabel = '节点指令',
  attachmentNodes,
  availableContextNodes,
  canSubmit,
  hasError,
  isContextPicking,
  isStreaming,
  isSubmitting,
  model,
  nodeKind,
  pendingAttachmentNodeIds,
  pendingInput,
  prompt,
  className,
  placeholder = '告诉这个节点接下来要做什么',
  showContextPicker = true,
  isResponding,
  onContextNodeRemove,
  onContextPickerToggle,
  onModelChange,
  onPromptChange,
  onPriorityContextNodeAdd,
  onRespond,
  onSubmit,
}: CanvasAgentPromptInputProps) {
  const [answer, setAnswer] = useState('')
  const [selectedOption, setSelectedOption] = useState('')
  const promptDraftRef = useRef(prompt)
  const promptEditorRef = useRef<CanvasPromptEditorHandle>(null)
  useEffect(() => {
    setAnswer('')
    setSelectedOption('')
  }, [pendingInput?.approvalEventId])

  const isAnswerMode = pendingInput !== null
  useEffect(() => {
    for (const nodeId of promptDraftRef.current.contextNodeIds) {
      if (attachmentNodeIds.has(nodeId) || pendingAttachmentNodeIds.has(nodeId)) continue
      promptEditorRef.current?.removeContextNode(nodeId)
    }
  }, [attachmentNodeIds, pendingAttachmentNodeIds])

  const responseText = [selectedOption, answer.trim()].filter(Boolean).join('\n')
  const status: ChatStatus = isSubmitting || isResponding
    ? 'submitted'
    : isStreaming
      ? 'streaming'
      : hasError
        ? 'error'
        : 'ready'
  const submitPrompt = () => {
    if (pendingInput !== null) {
      if (responseText === '' || isResponding) return
      onRespond(responseText)
      return
    }
    const nextPrompt = promptDraftRef.current.requestText.trim()
    if (!canSubmit || nextPrompt === '') return
    onSubmit({
      contextNodeIds: promptDraftRef.current.contextNodeIds.filter((nodeId) => attachmentNodeIds.has(nodeId)),
      prompt: nextPrompt,
    })
  }

  const handlePromptChange = (value: CanvasPromptValue) => {
    const previousContextNodeIds = new Set(promptDraftRef.current.contextNodeIds)
    promptDraftRef.current = value
    for (const nodeId of value.contextNodeIds) {
      if (!previousContextNodeIds.has(nodeId)) onPriorityContextNodeAdd(nodeId)
    }
    onPromptChange(value)
  }

  return (
    <TooltipProvider>
      <PromptInput
        className={cn('[&_[data-slot=input-group]]:relative [&_[data-slot=input-group]]:min-h-0 [&_[data-slot=input-group]]:items-stretch [&_[data-slot=input-group]]:overflow-visible [&_[data-slot=input-group]]:rounded-[calc(var(--radius-md)+var(--spacing-space-sm))] [&_[data-slot=input-group]]:border-0 [&_[data-slot=input-group]]:bg-canvas-elevated [&_[data-slot=input-group]]:shadow-none', className)}
        data-node-kind={nodeKind}
        onSubmit={submitPrompt}
      >
        <PromptInputHeader className={isAnswerMode ? 'p-space-sm pb-0' : 'p-0'}>
          {pendingInput === null ? (
            <CanvasContextNodes
              disabled={isSubmitting || isStreaming}
              isPicking={isContextPicking}
              nodes={attachmentNodes}
              showPicker={showContextPicker}
              onPickerToggle={onContextPickerToggle}
              onRemove={(nodeId) => {
                promptEditorRef.current?.removeContextNode(nodeId)
                onContextNodeRemove(nodeId)
              }}
            />
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
              className="min-h-28 max-h-48 px-space-md py-space-sm text-body-md leading-6 text-ink placeholder:text-faint"
              onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setAnswer(event.currentTarget.value)}
              onPaste={preserveTextOnlyPaste}
              placeholder="补充你的回答（可选）"
              value={answer}
            />
          ) : (
            <CanvasPromptEditor
              ref={promptEditorRef}
              ariaLabel={ariaLabel}
              availableContextNodes={availableContextNodes}
              disabled={isSubmitting || isStreaming}
              initialValue={prompt}
              placeholder={placeholder}
              onChange={handlePromptChange}
            />
          )}
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
