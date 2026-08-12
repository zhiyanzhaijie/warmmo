import { Check, BrainCircuit, History, MapPin, Plus, Sparkles } from 'lucide-react'
import { memo, useCallback, useEffect, useMemo, useState } from 'react'

import { type CollaborativeAgentTarget } from '@/apis/canvas-apis'
import { Message, MessageContent, MessageResponse } from '@/components/ai-elements/message'
import {
	Reasoning,
	ReasoningContent,
	ReasoningTrigger,
} from '@/components/ai-elements/reasoning'
import { Shimmer } from '@/components/ai-elements/shimmer'
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { CollaborativeAgentPromptInput } from '@/features/canvas/agent-workspace/CollaborativeAgentPromptInput'
import { CollaborativeAgentProgress } from '@/features/canvas/agent-workspace/CollaborativeAgentProgress'
import {
	useAgentStreamText,
} from '@/features/canvas/agent-workspace/agent-stream-store'
import {
  createCanvasPromptValueFromText,
  type CanvasPromptValue,
} from '@/features/canvas/agent-workspace/CanvasPromptEditor'
import {
  useCollaborativeAgentSession,
  type CollaborativeTurn,
} from '@/features/canvas/agent-workspace/use-collaborative-agent-session'
import { useAutoFollow } from '@/features/canvas/agent-workspace/use-auto-follow'
import type { CanvasAgentPromptSubmission } from '@/features/canvas/agent-workspace/types'
import {
  isCollaborativeContextNodePicker,
  useFlowNodeStore,
} from '@/features/canvas/flownode/store'
import { useFocusNode } from '@/features/canvas/flownode/use-focus-node'
import type { CanvasNode } from '@/types/canvas'
import type { AgentEvent } from '@/types/canvas'
import type { EnabledModel } from '@/types/provider'

interface CollaborativeAgentDrawerProps {
  canvasNodes: CanvasNode[]
  model: EnabledModel | null
  workId: string
  onModelChange: (model: EnabledModel | null) => void
}

const emptyPrompt = createCanvasPromptValueFromText('')

const starterPrompts = [
  '基于当前故事脉络，规划下一章的关键冲突',
  '寻找画布中尚未兑现的伏笔和角色关系',
  '为当前故事提出三个可继续发展的方向',
]

const sessionTimeFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: 'numeric', day: 'numeric', hour: 'numeric', minute: '2-digit',
})

export const CollaborativeAgentDrawer = memo(function CollaborativeAgentDrawer({
  canvasNodes,
  model,
  workId,
  onModelChange,
}: CollaborativeAgentDrawerProps) {
  const canvasInteractionMode = useFlowNodeStore((state) => state.canvasInteractionMode)
  const collaborativeContextNodeIds = useFlowNodeStore((state) => state.collaborativeContextNodeIds)
  const addContextNode = useFlowNodeStore((state) => state.actions.addCollaborativeContextNode)
  const removeContextNode = useFlowNodeStore((state) => state.actions.removeCollaborativeContextNode)
  const clearContextNodes = useFlowNodeStore((state) => state.actions.clearCollaborativeContextNodes)
  const startContextNodePicker = useFlowNodeStore((state) => state.actions.startContextNodePicker)
  const cancelContextNodePicker = useFlowNodeStore((state) => state.actions.cancelContextNodePicker)
  const {
    activeTurn,
    canUseContextAgent,
    conversation,
    conversationSessionId,
    createConversationSession,
    contextAgentPending,
    pendingInput,
    responding,
    respond,
    run,
    selectSession,
    turns,
  } = useCollaborativeAgentSession(workId)
  const focusNode = useFocusNode()
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<CollaborativeAgentTarget>('collaborative-targeted')
  const [prompt, setPrompt] = useState<CanvasPromptValue>(emptyPrompt)
  const [composerVersion, setComposerVersion] = useState(0)
  const setScrollViewport = useAutoFollow(open)
  const contextNodeIds = useMemo(() => new Set(collaborativeContextNodeIds), [collaborativeContextNodeIds])
  const contextNodes = useMemo(() => canvasNodes.filter((node) => contextNodeIds.has(node.id)), [canvasNodes, contextNodeIds])
  const isContextPicking = isCollaborativeContextNodePicker(canvasInteractionMode)
  const canSubmit = model !== null && prompt.requestText.trim() !== '' && activeTurn === undefined && canUseContextAgent
  const activeSession = conversation?.sessions.find((session) => session.id === conversationSessionId)
  const contextUsage = activeSession?.latestUsage ?? [...turns].reverse().find((turn) => turn.usage !== undefined)?.usage

  const updatePrompt = useCallback((value: CanvasPromptValue) => {
    setPrompt(value)
  }, [])

  const toggleContextNodePicker = useCallback(() => {
    if (isContextPicking) {
      cancelContextNodePicker()
      return
    }
    startContextNodePicker({ kind: 'collaborative-agent' })
  }, [cancelContextNodePicker, isContextPicking, startContextNodePicker])

  useEffect(() => {
    if (open || !isContextPicking) return
    cancelContextNodePicker()
  }, [cancelContextNodePicker, isContextPicking, open])

  useEffect(() => {
    const availableNodeIds = new Set(canvasNodes.map((node) => node.id))
    for (const nodeId of collaborativeContextNodeIds) {
      if (!availableNodeIds.has(nodeId)) removeContextNode(nodeId)
    }
  }, [canvasNodes, collaborativeContextNodeIds, removeContextNode])

  const submit = useCallback((input: CanvasAgentPromptSubmission) => {
    if (model === null || !canSubmit) return
    run({
      contextNodeIds: input.contextNodeIds,
      model,
      prompt: input.prompt,
      target: mode,
    })
    setPrompt(emptyPrompt)
    clearContextNodes()
    if (isContextPicking) cancelContextNodePicker()
    setComposerVersion((current) => current + 1)
  }, [canSubmit, cancelContextNodePicker, clearContextNodes, isContextPicking, mode, model, run])

  const selectStarter = useCallback((value: string) => {
    setPrompt(createCanvasPromptValueFromText(value))
    setComposerVersion((current) => current + 1)
  }, [])

  const locateCandidate = useCallback((candidateId: string) => {
    focusNode(`candidate:${candidateId}`)
  }, [focusNode])

  const clearConversation = useCallback(() => {
    createConversationSession()
    setPrompt(emptyPrompt)
    clearContextNodes()
    if (isContextPicking) cancelContextNodePicker()
    setComposerVersion((current) => current + 1)
  }, [cancelContextNodePicker, clearContextNodes, createConversationSession, isContextPicking])

  const handleSelectSession = useCallback((sessionId: string) => {
    selectSession(sessionId)
    setPrompt(emptyPrompt)
    clearContextNodes()
    if (isContextPicking) cancelContextNodePicker()
    setComposerVersion((current) => current + 1)
  }, [cancelContextNodePicker, clearContextNodes, isContextPicking, selectSession])

  const availabilityLabel = contextAgentPending
    ? '正在检查上下文能力'
    : canUseContextAgent
      ? '全局创作 Agent'
      : '配置 Embedding Provider 后可用'

  return (
    <TooltipProvider delayDuration={180}>
      <Drawer
        direction="right"
        handleOnly
        modal={false}
        open={open}
        onOpenChange={setOpen}
      >
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

        <DrawerContent
          className="gap-0 bg-canvas shadow-none [&>[data-slot=drawer-close]]:top-space-xs [&>[data-slot=drawer-close]]:right-space-sm"
          defaultWidth={512}
          maxWidth={768}
          minWidth={384}
          resizable
          showOverlay={false}
        >
        <DrawerHeader className="flex h-12 items-center gap-space-xs px-space-sm py-0 pr-12">
          <span className="grid size-7 place-items-center rounded-sm bg-primary text-on-primary">
            <Sparkles aria-hidden="true" size={14} />
          </span>
          <DrawerTitle className="sr-only">全局创作 Agent</DrawerTitle>
          <DrawerDescription className="sr-only">与全局 Agent 闲聊或进行创作</DrawerDescription>
          <span className="min-w-0 flex-1 truncate text-body-sm text-body">{activeSession?.title ?? '新会话'}</span>
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button aria-label="查看历史会话" disabled={activeTurn !== undefined} size="icon-sm" variant="ghost">
                    <History aria-hidden="true" size={15} />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>历史会话</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end" className="w-72">
              <DropdownMenuLabel>画布历史会话</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {(conversation?.sessions ?? []).length === 0 ? (
                <DropdownMenuItem disabled>暂无历史会话</DropdownMenuItem>
              ) : (conversation?.sessions ?? []).map((session) => (
                <DropdownMenuItem key={session.id} onSelect={() => handleSelectSession(session.id)}>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-body-sm text-ink">{session.title}</p>
                    <p className="truncate text-mono-caption text-mute">{session.turnCount} 轮 · {session.modelId || '模型未记录'} · {formatSessionTime(session.updatedAt)}</p>
                  </div>
                  {session.id === conversationSessionId ? <Check aria-hidden="true" size={14} /> : null}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                aria-label="新建会话"
                disabled={activeTurn !== undefined}
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
              {turns.map((turn) => (
                <CollaborativeConversationTurn
                  key={turn.clientId}
                  onLocateCandidate={locateCandidate}
                  turn={turn}
                />
              ))}
            </div>
          )}
        </div>

        <div className="shrink-0 border-t border-hairline bg-canvas-elevated p-space-sm">
          <CollaborativeAgentPromptInput
            key={pendingInput?.approvalEventId ?? composerVersion}
            attachmentNodeIds={contextNodeIds}
            attachmentNodes={contextNodes}
            availableContextNodes={canvasNodes}
            canSubmit={canSubmit}
            contextUsage={contextUsage}
            contextWindowTokens={activeSession?.contextWindowTokens}
            modelId={activeSession?.modelId || (model === null ? undefined : `${model.providerId}:${model.modelId}`)}
            isContextPicking={isContextPicking}
            isResponding={responding}
            isStreaming={activeTurn?.status === 'running'}
            isSubmitting={activeTurn?.status === 'submitting'}
            mode={mode}
            model={model}
            pendingInput={pendingInput}
            prompt={prompt}
            onContextNodeAdd={addContextNode}
            onContextNodeRemove={removeContextNode}
            onContextPickerToggle={toggleContextNodePicker}
            onModeChange={setMode}
            onModelChange={onModelChange}
            onPromptChange={updatePrompt}
            onRespond={respond}
            onSubmit={submit}
          />
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
          {mode === 'collaborative-targeted' ? '从创作目标开始' : '从一个想法开始聊'}
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

const CollaborativeConversationTurn = memo(function CollaborativeConversationTurn({
  onLocateCandidate,
  turn,
}: {
  onLocateCandidate: (candidateId: string) => void
  turn: CollaborativeTurn
}) {
  const candidateEvents = turn.events.filter((event) => event.type === 'candidate.created')
  return (
    <div className="space-y-space-md">
      <Message from="user" className="max-w-[88%]">
        <MessageContent className="rounded-sm bg-hairline-soft px-space-sm py-space-sm text-body-md leading-6 text-ink">
          {turn.prompt}
        </MessageContent>
      </Message>
      <Message from="assistant" className="max-w-full">
        <MessageContent className="w-full gap-space-md text-body-md leading-6">
          <CollaborativeTurnTimeline turn={turn} />
          <StreamingCollaborativeResponse messageId={turn.clientId} running={turn.status === 'running'} />
          {turn.historyResponse === undefined ? null : (
            <MessageResponse className="text-body-md leading-6 text-ink" mode="static">
              {formatCollaborativeResponse(turn.historyResponse)}
            </MessageResponse>
          )}
          <CollaborativeCandidateMilestones events={candidateEvents} onLocateCandidate={onLocateCandidate} />
          {turn.error === undefined ? null : (
            <p className="border-l border-error pl-space-sm text-body-sm text-error" role="alert">{turn.error}</p>
          )}
        </MessageContent>
      </Message>
    </div>
  )
})

type TurnTimelineItem =
  | { id: string; type: 'reasoning'; text: string; streaming: boolean }
  | { id: string; type: 'collaboration'; events: AgentEvent[] }

const CollaborativeTurnTimeline = memo(function CollaborativeTurnTimeline({
  turn,
}: {
  turn: CollaborativeTurn
}) {
  const items = useMemo(() => buildTurnTimeline(turn.events), [turn.events])
  return items.map((item, index) => {
    if (item.type === 'reasoning') {
      return (
        <Reasoning
          key={item.id}
          className="mb-0 border-l border-hairline pl-space-sm"
          defaultOpen={item.streaming}
          isStreaming={item.streaming}
        >
          <ReasoningTrigger
            className="w-fit gap-space-xs text-body-sm text-mute"
            getThinkingMessage={reasoningStatusLabel}
          />
          {item.text === '' ? null : (
            <ReasoningContent className="mt-space-xs max-h-56 overflow-y-auto pr-space-xs text-body-sm leading-6 text-mute">
              {item.text}
            </ReasoningContent>
          )}
        </Reasoning>
      )
    }
    const isLast = index === items.length - 1
    return (
      <CollaborativeAgentProgress
        key={item.id}
        turn={{ ...turn, events: item.events, status: isLast ? turn.status : 'completed' }}
      />
    )
  })
})

function buildTurnTimeline(events: AgentEvent[]): TurnTimelineItem[] {
  const reasoningEvents = events.filter((event) => event.type.startsWith('reasoning.'))
  const collaborationEvents = events.filter((event) => isCollaborationTimelineEvent(event.type))
  const items: Array<TurnTimelineItem & { sequence: number }> = []

  if (reasoningEvents.length > 0) {
    const lastLifecycleEvent = reasoningEvents.findLast((event) =>
      event.type === 'reasoning.started' || event.type === 'reasoning.completed')
    items.push({
      id: 'reasoning',
      type: 'reasoning',
      text: reasoningEvents.flatMap((event) =>
        event.type === 'reasoning.delta' && typeof event.data?.delta === 'string' ? [event.data.delta] : []).join('\n\n'),
      streaming: lastLifecycleEvent?.type === 'reasoning.started',
      sequence: reasoningEvents[0].sequence,
    })
  }
  if (collaborationEvents.length > 0) {
    items.push({
      id: 'collaboration',
      type: 'collaboration',
      events: collaborationEvents,
      sequence: collaborationEvents[0].sequence,
    })
  }
  return items.sort((left, right) => left.sequence - right.sequence)
}

function isCollaborationTimelineEvent(type: string) {
  return type === 'context.preparing' || type === 'context.ready' ||
    type === 'plan.started' || type === 'plan.completed' ||
    type === 'role.started' || type === 'role.handoff' || type === 'role.completed' ||
    type === 'tool.requested' || type === 'tool.started' ||
    type === 'tool.completed' || type === 'tool.failed'
}

function reasoningStatusLabel(isStreaming: boolean, duration?: number) {
	if (isStreaming) {
		return (
			<Shimmer
				as="span"
				className="font-medium [--color-background:var(--color-ink)] [--color-muted-foreground:var(--color-mute)]"
				duration={1.5}
				spread={8}
			>
				正在思考
			</Shimmer>
		)
	}
	return <span>{duration === undefined ? '思考过程' : `思考了 ${duration} 秒`}</span>
}

const CollaborativeCandidateMilestones = memo(function CollaborativeCandidateMilestones({
	events,
	onLocateCandidate,
}: {
	events: CollaborativeTurn['events']
	onLocateCandidate: (candidateId: string) => void
}) {
	if (events.length === 0) return null
	return (
		<div aria-live="polite" className="space-y-space-xs border-t border-hairline pt-space-sm">
			{events.map((event) => {
				const candidateId = typeof event.data?.candidateId === 'string' ? event.data.candidateId : null
				if (candidateId === null) return null
				const meta = event.data?.meta as Record<string, unknown> | undefined
				const title = typeof meta?.title === 'string' ? meta.title : '新候选节点'
				const ordinal = typeof meta?.ordinal === 'number' && typeof meta?.total === 'number'
					? `${meta.ordinal}/${meta.total}`
					: null
				return (
					<div key={event.id} className="flex min-w-0 items-center gap-space-xs text-body-sm text-body">
						<span className="grid size-5 shrink-0 place-items-center rounded-full bg-link-soft text-link">
							<Check aria-hidden="true" size={12} strokeWidth={2.25} />
						</span>
						<span className="min-w-0 flex-1 truncate">
							已生成候选「{title}」{ordinal === null ? null : ` · ${ordinal}`}
						</span>
						<Tooltip>
							<TooltipTrigger asChild>
								<Button
									aria-label={`定位候选 ${title}`}
									className="shrink-0"
									size="icon-sm"
									variant="ghost"
									onClick={() => onLocateCandidate(candidateId)}
								>
									<MapPin aria-hidden="true" size={14} />
								</Button>
							</TooltipTrigger>
							<TooltipContent>在画布中定位</TooltipContent>
						</Tooltip>
					</div>
				)
			})}
		</div>
	)
})

const StreamingCollaborativeResponse = memo(function StreamingCollaborativeResponse({
  messageId,
  running,
}: {
  messageId: string
  running: boolean
}) {
  const response = useAgentStreamText(messageId)
  const formattedResponse = useMemo(() => formatCollaborativeResponse(response), [response])
  if (response === '') return null
  return (
    <MessageResponse
      animated={{ animation: 'fadeIn', duration: 180, sep: 'word', stagger: 12 }}
      className="text-body-md leading-6 text-ink"
      isAnimating={running}
      mode="streaming"
      parseIncompleteMarkdown
    >
      {formattedResponse}
    </MessageResponse>
  )
})

function formatCollaborativeResponse(response: string) {
  if (!response.trimStart().startsWith('{')) return response
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

function formatSessionTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return sessionTimeFormatter.format(date)
}
