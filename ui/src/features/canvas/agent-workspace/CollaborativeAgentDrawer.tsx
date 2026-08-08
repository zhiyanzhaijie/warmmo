import { BrainCircuit, Compass, Feather, Layers3, Plus, Sparkles } from 'lucide-react'
import { memo, useCallback, useLayoutEffect, useMemo, useState } from 'react'

import { type CollaborativeAgentTarget } from '@/apis/canvas-apis'
import { Message, MessageContent, MessageResponse } from '@/components/ai-elements/message'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from '@/components/ui/drawer'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import {
  CanvasAgentPromptInput,
  type CanvasAgentPromptSubmission,
} from '@/features/canvas/agent-workspace/AgentPromptInput'
import { CollaborativeAgentProgress } from '@/features/canvas/agent-workspace/CollaborativeAgentProgress'
import {
  createCanvasPromptValueFromText,
  type CanvasPromptValue,
} from '@/features/canvas/agent-workspace/CanvasPromptEditor'
import {
  getCollaborativeResponse,
  useCollaborativeAgentSession,
  type CollaborativeTurn,
} from '@/features/canvas/agent-workspace/use-collaborative-agent-session'
import { useFlowNodeStore } from '@/features/canvas/flownode/store'
import type { CanvasNode } from '@/types/canvas'
import type { EnabledModel } from '@/types/provider'

interface CollaborativeAgentDrawerProps {
  canvasNodes: CanvasNode[]
  model: EnabledModel | null
  workId: string
  onModelChange: (model: EnabledModel | null) => void
}

const emptyPrompt = createCanvasPromptValueFromText('')
const emptyNodeIdSet: ReadonlySet<string> = new Set()

const modes: Array<{
  icon: typeof Feather
  label: string
  target: CollaborativeAgentTarget
}> = [
  { icon: Feather, label: '目标创作', target: 'collaborative-targeted' },
  { icon: Compass, label: '灵感探索', target: 'collaborative-explore' },
]

const starterPrompts = [
  '基于当前故事脉络，规划下一章的关键冲突',
  '寻找画布中尚未兑现的伏笔和角色关系',
  '为当前故事提出三个可继续发展的方向',
]

export const CollaborativeAgentDrawer = memo(function CollaborativeAgentDrawer({
  canvasNodes,
  model,
  workId,
  onModelChange,
}: CollaborativeAgentDrawerProps) {
  const selectedNodeIds = useFlowNodeStore((state) => state.selectedSourceNodeIds)
  const {
    activeTurn,
    canUseContextAgent,
    clear,
    contextAgentPending,
    pendingInput,
    responding,
    respond,
    run,
    turns,
  } = useCollaborativeAgentSession(workId)
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<CollaborativeAgentTarget>('collaborative-targeted')
  const [prompt, setPrompt] = useState<CanvasPromptValue>(emptyPrompt)
  const [composerVersion, setComposerVersion] = useState(0)
  const [manualContextNodeIds, setManualContextNodeIds] = useState<ReadonlySet<string>>(() => new Set())
  const scrollViewportRef = useState<HTMLDivElement | null>(null)
  const [scrollViewport, setScrollViewport] = scrollViewportRef
  const contextNodeIds = useMemo(() => new Set([...selectedNodeIds, ...manualContextNodeIds]), [manualContextNodeIds, selectedNodeIds])
  const contextNodes = useMemo(() => canvasNodes.filter((node) => contextNodeIds.has(node.id)), [canvasNodes, contextNodeIds])
  const canSubmit = model !== null && prompt.requestText.trim() !== '' && activeTurn === undefined && canUseContextAgent

  useLayoutEffect(() => {
    if (scrollViewport === null || !open) return
    scrollViewport.scrollTop = scrollViewport.scrollHeight
  }, [open, scrollViewport, turns])

  const updatePrompt = useCallback((value: CanvasPromptValue) => {
    setPrompt(value)
  }, [])

  const addContextNode = useCallback((nodeId: string) => {
    setManualContextNodeIds((current) => current.has(nodeId) ? current : new Set([...current, nodeId]))
  }, [])

  const removeContextNode = useCallback((nodeId: string) => {
    setManualContextNodeIds((current) => {
      if (!current.has(nodeId)) return current
      const next = new Set(current)
      next.delete(nodeId)
      return next
    })
  }, [])

  const submit = useCallback((input: CanvasAgentPromptSubmission) => {
    if (model === null || !canSubmit) return
    run({
      contextNodeIds: [...new Set([...selectedNodeIds, ...input.contextNodeIds])],
      model,
      prompt: input.prompt,
      target: mode,
    })
    setPrompt(emptyPrompt)
    setManualContextNodeIds(new Set())
    setComposerVersion((current) => current + 1)
  }, [canSubmit, mode, model, run, selectedNodeIds])

  const selectStarter = useCallback((value: string) => {
    setPrompt(createCanvasPromptValueFromText(value))
    setComposerVersion((current) => current + 1)
  }, [])

  const clearConversation = useCallback(() => {
    clear()
    setPrompt(emptyPrompt)
    setManualContextNodeIds(new Set())
    setComposerVersion((current) => current + 1)
  }, [clear])

  const availabilityLabel = contextAgentPending
    ? '正在检查上下文能力'
    : canUseContextAgent
      ? '全局创作 Agent'
      : '配置 Embedding Provider 后可用'

  return (
    <TooltipProvider delayDuration={180}>
      <Drawer open={open} onOpenChange={setOpen}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="fixed top-space-md right-space-md z-40">
              <DrawerTrigger asChild>
                <Button
                  aria-label="打开全局创作 Agent"
                  className="bg-canvas-elevated/95 text-ink backdrop-blur-sm hover:bg-hairline-soft"
                  disabled={contextAgentPending || !canUseContextAgent}
                  size="icon-lg"
                  variant="outline"
                >
                  <BrainCircuit aria-hidden="true" size={17} />
                </Button>
              </DrawerTrigger>
            </span>
          </TooltipTrigger>
          <TooltipContent side="left">{availabilityLabel}</TooltipContent>
        </Tooltip>

        <DrawerContent side="right" className="gap-0 bg-canvas shadow-none sm:w-[32rem]">
        <DrawerHeader className="flex h-14 items-center gap-space-sm px-space-md py-0 pr-14">
          <span className="grid size-7 place-items-center rounded-sm bg-primary text-on-primary">
            <Sparkles aria-hidden="true" size={14} />
          </span>
          <div className="min-w-0 flex-1">
            <DrawerTitle className="text-label-sm">创作协作</DrawerTitle>
            <DrawerDescription className="mt-0 font-mono text-[0.625rem] uppercase text-faint">
              Planner / Creator / Writer
            </DrawerDescription>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                aria-label="新建会话"
                disabled={activeTurn !== undefined || turns.length === 0}
                size="icon-sm"
                variant="ghost"
                onClick={clearConversation}
              >
                <Plus aria-hidden="true" size={15} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>新建会话</TooltipContent>
          </Tooltip>
        </DrawerHeader>

        <div ref={setScrollViewport} className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          {turns.length === 0 ? (
            <CollaborativeAgentEmptyState mode={mode} onSelect={selectStarter} />
          ) : (
            <div className="mx-auto flex w-full max-w-[44rem] flex-col gap-space-xl px-space-lg py-space-xl">
              {turns.map((turn) => <CollaborativeConversationTurn key={turn.clientId} turn={turn} />)}
            </div>
          )}
        </div>

        <div className="shrink-0 border-t border-hairline bg-canvas-elevated p-space-md">
          {pendingInput !== null ? (
            <CanvasAgentPromptInput
              key={pendingInput.approvalEventId}
              ariaLabel="回答创作 Agent"
              attachmentNodeIds={contextNodeIds}
              attachmentNodes={contextNodes}
              availableContextNodes={canvasNodes}
              canSubmit
              className="[&_[data-slot=input-group]]:rounded-sm [&_[data-slot=input-group]]:border [&_[data-slot=input-group]]:border-hairline"
              hasError={false}
              isContextPicking={false}
              isResponding={responding}
              isStreaming={false}
              isSubmitting={false}
              model={model}
              nodeKind="collaborative"
              pendingAttachmentNodeIds={emptyNodeIdSet}
              pendingInput={pendingInput}
              prompt={prompt}
              showContextPicker={false}
              onContextNodeRemove={removeContextNode}
              onContextPickerToggle={() => undefined}
              onModelChange={onModelChange}
              onPriorityContextNodeAdd={addContextNode}
              onPromptChange={updatePrompt}
              onRespond={respond}
              onSubmit={submit}
            />
          ) : (
            <>
              <div className="mb-space-sm flex items-center justify-between gap-space-sm">
                <div className="flex rounded-sm bg-hairline-soft p-px" role="group" aria-label="协作模式">
                  {modes.map((option) => {
                    const Icon = option.icon
                    const active = option.target === mode
                    return (
                      <Button
                        key={option.target}
                        aria-pressed={active}
                        className={active ? 'bg-canvas-elevated text-ink shadow-whisper hover:bg-canvas-elevated' : 'text-mute'}
                        disabled={activeTurn !== undefined}
                        size="xs"
                        variant="ghost"
                        onClick={() => setMode(option.target)}
                      >
                        <Icon aria-hidden="true" size={12} />
                        {option.label}
                      </Button>
                    )
                  })}
                </div>
                {selectedNodeIds.length > 0 ? (
                  <span className="flex items-center gap-space-xxs text-body-sm text-mute">
                    <Layers3 aria-hidden="true" size={13} />
                    {selectedNodeIds.length} 个焦点节点
                  </span>
                ) : null}
              </div>
              <CanvasAgentPromptInput
                key={composerVersion}
                ariaLabel="给全局创作 Agent 的消息"
                attachmentNodeIds={contextNodeIds}
                attachmentNodes={contextNodes}
                availableContextNodes={canvasNodes}
                canSubmit={canSubmit}
                className="border border-hairline [&_[data-slot=input-group]]:rounded-sm [&_[data-slot=input-group]]:border-0"
                hasError={false}
                isContextPicking={false}
                isResponding={false}
                isStreaming={activeTurn?.status === 'running'}
                isSubmitting={activeTurn?.status === 'submitting'}
                model={model}
                nodeKind="collaborative"
                pendingAttachmentNodeIds={emptyNodeIdSet}
                pendingInput={null}
                placeholder={mode === 'collaborative-targeted' ? '描述你想完成的创作目标' : '从一个问题或模糊想法开始'}
                prompt={prompt}
                showContextPicker={false}
                onContextNodeRemove={removeContextNode}
                onContextPickerToggle={() => undefined}
                onModelChange={onModelChange}
                onPriorityContextNodeAdd={addContextNode}
                onPromptChange={updatePrompt}
                onRespond={() => undefined}
                onSubmit={submit}
              />
            </>
          )}
        </div>
        </DrawerContent>
      </Drawer>
    </TooltipProvider>
  )
})

function CollaborativeAgentEmptyState({
  mode,
  onSelect,
}: {
  mode: CollaborativeAgentTarget
  onSelect: (prompt: string) => void
}) {
  return (
    <div className="flex min-h-full flex-col justify-end px-space-lg pb-space-xl sm:justify-center">
      <div className="mb-space-xl">
        <BrainCircuit aria-hidden="true" className="text-faint" size={26} strokeWidth={1.4} />
        <h2 className="mt-space-md text-heading-md text-ink">
          {mode === 'collaborative-targeted' ? '从意图开始创作' : '从关系中发现可能'}
        </h2>
      </div>
      <div className="divide-y divide-hairline border-y border-hairline">
        {starterPrompts.map((prompt) => (
          <Button
            key={prompt}
            className="h-auto w-full justify-between whitespace-normal px-0 py-space-sm text-left text-body-md font-normal text-body hover:bg-transparent hover:text-ink"
            variant="ghost"
            onClick={() => onSelect(prompt)}
          >
            <span>{prompt}</span>
            <Plus aria-hidden="true" className="text-faint" size={14} />
          </Button>
        ))}
      </div>
    </div>
  )
}

function CollaborativeConversationTurn({ turn }: { turn: CollaborativeTurn }) {
  const response = getCollaborativeResponse(turn.events)
  return (
    <div className="space-y-space-md">
      <Message from="user" className="max-w-[88%]">
        <MessageContent className="rounded-sm bg-hairline-soft px-space-sm py-space-sm text-body-md leading-6 text-ink">
          {turn.prompt}
        </MessageContent>
      </Message>
      <Message from="assistant" className="max-w-full">
        <MessageContent className="w-full gap-space-md text-body-md leading-6">
          <CollaborativeAgentProgress turn={turn} />
          {response === '' ? null : (
            <MessageResponse className="text-body-md leading-6 text-ink">
              {formatCollaborativeResponse(response)}
            </MessageResponse>
          )}
          {turn.error === undefined ? null : (
            <p className="border-l border-error pl-space-sm text-body-sm text-error" role="alert">{turn.error}</p>
          )}
        </MessageContent>
      </Message>
    </div>
  )
}

function formatCollaborativeResponse(response: string) {
  try {
    const proposal = JSON.parse(response) as {
      nodes?: Array<{ content?: string; kind?: string; reason?: string; title?: string }>
      updates?: Array<{ content?: string; reason?: string; title?: string }>
    }
    const sections: string[] = []
    if (Array.isArray(proposal.nodes) && proposal.nodes.length > 0) {
      sections.push('## 新节点')
      for (const node of proposal.nodes) {
        sections.push(`### ${node.title ?? '未命名节点'}${node.kind === undefined ? '' : ` · ${node.kind}`}`)
        if (node.reason !== undefined) sections.push(`> ${node.reason}`)
        if (node.content !== undefined) sections.push(node.content)
      }
    }
    if (Array.isArray(proposal.updates) && proposal.updates.length > 0) {
      sections.push('## 节点更新')
      for (const update of proposal.updates) {
        sections.push(`### ${update.title ?? '节点更新'}`)
        if (update.reason !== undefined) sections.push(`> ${update.reason}`)
        if (update.content !== undefined) sections.push(update.content)
      }
    }
    return sections.length === 0 ? response : sections.join('\n\n')
  } catch {
    return response
  }
}
