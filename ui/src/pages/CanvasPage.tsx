import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type NodeMouseHandler,
  type OnSelectionChangeParams,
} from '@xyflow/react'
import { useQueryClient } from '@tanstack/react-query'
import { ChevronDown, LoaderCircle, Play, Plus, Sparkles, X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'

import '@xyflow/react/dist/style.css'

import {
  canvasKeys,
  useCanvasCandidates,
  useCanvasNodes,
  useCreateAgentRun,
  useCreateCanvasNode,
  useUpdateCanvasNodePosition,
} from '@/apis/canvas-apis'
import { ModelSelector } from '@/components/models/ModelSelector'
import { nodeVisuals, storyNodeTypes, type StoryFlowNode } from '@/features/canvas/story-node'
import type { AgentEvent, CanvasNodeKind } from '@/types/canvas'
import type { EnabledModel } from '@/types/provider'

const creatableKinds: CanvasNodeKind[] = ['character', 'setting', 'item', 'plot', 'world', 'note', 'timeline', 'chapter']

const eventLabels: Record<string, string> = {
  'run.queued': '进入队列', 'run.started': '开始运行', 'context.preparing': '准备上下文',
  'context.ready': '上下文就绪', 'brainstorm.started': '开始构思', 'brainstorm.completed': '完成构思',
  'plan.started': '开始规划', 'plan.completed': '规划完成', 'skill.searching': '检索 Skill',
  'skill.matched': '命中 Skill', 'skill.loaded': '加载 Skill', 'skill.completed': 'Skill 完成',
  'tool.requested': '请求 Tool', 'tool.started': '调用 Tool', 'tool.completed': 'Tool 完成',
  'tool.failed': 'Tool 失败', 'approval.required': '等待用户确认', 'validation.completed': '校验完成',
  'candidate.created': '创建候选', 'run.completed': '运行完成', 'run.failed': '运行失败',
  'run.cancelled': '运行取消',
}
const streamedEventTypes = [...Object.keys(eventLabels), 'message.delta']

export function CanvasPage() {
  return <ReactFlowProvider><CanvasWorkspace /></ReactFlowProvider>
}

function CanvasWorkspace() {
  const { workId = 'local-work' } = useParams()
  const queryClient = useQueryClient()
  const flow = useReactFlow<StoryFlowNode>()
  const nodesQuery = useCanvasNodes(workId)
  const candidatesQuery = useCanvasCandidates(workId)
  const createNode = useCreateCanvasNode(workId)
  const updatePosition = useUpdateCanvasNodePosition(workId)
  const createRun = useCreateAgentRun(workId)
  const [nodes, setNodes, onNodesChange] = useNodesState<StoryFlowNode>([])
  const [edges, , onEdgesChange] = useEdgesState([])
  const [selectedNodeIds, setSelectedNodeIds] = useState<string[]>([])
  const [model, setModel] = useState<EnabledModel | null>(null)
  const [prompt, setPrompt] = useState('让主角在宴会上第一次发现记忆能力的代价。')
  const [creatingKind, setCreatingKind] = useState<CanvasNodeKind | null>(null)
  const [nodeTitle, setNodeTitle] = useState('')
  const [nodeContent, setNodeContent] = useState('')
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [draft, setDraft] = useState('')
  const [activeRunId, setActiveRunId] = useState<string | null>(null)
  const [streamError, setStreamError] = useState('')
  const eventSourceRef = useRef<EventSource | null>(null)
  const fittedInitialNodesRef = useRef(false)

  const remoteNodes = useMemo<StoryFlowNode[]>(() => {
    const sourceNodes = (nodesQuery.data ?? []).map<StoryFlowNode>((node) => ({
      id: node.id,
      type: 'story',
      position: { x: node.x, y: node.y },
      data: {
        sourceId: node.id, sourceType: 'node', kind: node.kind, title: node.title,
        content: node.content, revision: node.revision, layerId: 'main', contextTags: [],
      },
    }))
    const candidateNodes = (candidatesQuery.data ?? []).map<StoryFlowNode>((candidate, index) => ({
      id: `candidate:${candidate.id}`,
      type: 'story',
      position: { x: 520 + index * 36, y: 80 + index * 36 },
      draggable: false,
      data: {
        sourceId: candidate.id, sourceType: 'candidate', kind: 'chapter', title: '章节候选',
        content: candidate.content, revision: 0, layerId: 'candidate', contextTags: [],
      },
    }))
    return [...sourceNodes, ...candidateNodes]
  }, [candidatesQuery.data, nodesQuery.data])

  useEffect(() => {
    setNodes((current) => {
      const currentById = new Map(current.map((node) => [node.id, node]))
      return remoteNodes.map((node) => {
        const existing = currentById.get(node.id)
        return existing === undefined ? node : { ...node, position: existing.position, selected: existing.selected }
      })
    })
  }, [remoteNodes, setNodes])

  useEffect(() => {
    if (fittedInitialNodesRef.current || remoteNodes.length === 0) return
    fittedInitialNodesRef.current = true
    requestAnimationFrame(() => void flow.fitView({ padding: 0.25, maxZoom: 1 }))
  }, [flow, remoteNodes.length])

  useEffect(() => () => eventSourceRef.current?.close(), [])

  const streamRun = useCallback((runId: string) => {
    eventSourceRef.current?.close()
    setActiveRunId(runId)
    setEvents([])
    setDraft('')
    setStreamError('')
    const source = new EventSource(`/api/v1/agent-runs/${runId}/events`)
    eventSourceRef.current = source
    const receive = (message: MessageEvent<string>) => {
      const event = JSON.parse(message.data) as AgentEvent
      if (event.type === 'message.delta' && typeof event.data?.delta === 'string') {
        setDraft((current) => current + event.data.delta)
      } else {
        setEvents((current) => [...current, event])
      }
      if (isTerminalEvent(event.type)) {
        source.close()
        setActiveRunId(null)
        void queryClient.invalidateQueries({ queryKey: canvasKeys.candidates(workId) })
      }
    }
    for (const type of streamedEventTypes) source.addEventListener(type, receive as EventListener)
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) return
      source.close()
      setActiveRunId(null)
      setStreamError('过程流连接已断开')
    }
  }, [queryClient, workId])

  const onSelectionChange = useCallback(({ nodes: selected }: OnSelectionChangeParams<StoryFlowNode>) => {
    setSelectedNodeIds(selected.flatMap((node) => node.data.sourceType === 'node' ? [node.data.sourceId] : []))
  }, [])

  const onNodeDragStop = useCallback<NodeMouseHandler<StoryFlowNode>>((_, node) => {
    if (node.data.sourceType !== 'node') return
    updatePosition.mutate({ nodeId: node.data.sourceId, x: node.position.x, y: node.position.y })
  }, [updatePosition])

  const openCreator = useCallback((kind: CanvasNodeKind) => {
    setCreatingKind(kind)
    setNodeTitle('')
    setNodeContent('')
  }, [])

  const createAtViewportCenter = useCallback(() => {
    if (creatingKind === null) return
    const position = flow.screenToFlowPosition({ x: window.innerWidth / 2, y: window.innerHeight / 2 })
    createNode.mutate({ kind: creatingKind, title: nodeTitle, content: nodeContent, x: position.x, y: position.y }, {
      onSuccess: () => setCreatingKind(null),
    })
  }, [createNode, creatingKind, flow, nodeContent, nodeTitle])

  const canRun = model !== null && prompt.trim() !== '' && selectedNodeIds.length > 0 && activeRunId === null

  return (
    <main className="relative h-dvh w-full overflow-hidden bg-canvas text-ink">
      <ReactFlow<StoryFlowNode>
        nodes={nodes}
        edges={edges}
        nodeTypes={storyNodeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onSelectionChange={onSelectionChange}
        onNodeDragStop={onNodeDragStop}
        fitView
        fitViewOptions={{ padding: 0.25, maxZoom: 1 }}
        minZoom={0.08}
        maxZoom={1.8}
        onlyRenderVisibleElements
        elevateNodesOnSelect
        selectionOnDrag
        panOnScroll
        className="warmnote-flow"
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="var(--color-hairline)" />
        <Controls position="bottom-right" showInteractive={false} />
        <MiniMap position="bottom-right" pannable zoomable className="!right-14 !h-24 !w-36 !rounded-sm !border !border-hairline !bg-canvas-elevated" nodeColor="var(--color-mute)" maskColor="color-mix(in srgb, var(--color-canvas) 76%, transparent)" />
      </ReactFlow>

      <header className="absolute inset-x-0 top-0 z-20 flex h-16 items-center justify-between border-b border-hairline bg-canvas-elevated/95 px-16 backdrop-blur-sm">
        <div className="min-w-0">
          <div className="font-mono text-mono-eyebrow text-mute">WARMNOTE / {workId}</div>
          <div className="truncate text-label-sm">{nodesQuery.data?.length ?? 0} 节点 · {selectedNodeIds.length} 已选</div>
        </div>
        <div className="flex items-center gap-space-xs">
          <ModelSelector capability="text" value={model} onValueChange={setModel} autoSelectFirst className="max-w-56" />
        </div>
      </header>

      <div className="absolute top-20 left-space-md z-20 flex items-center rounded-sm border border-hairline bg-canvas-elevated p-1 shadow-floating">
        <button className="flex h-8 items-center gap-space-xs rounded-sm px-space-sm text-button-md hover:bg-hairline-soft" type="button" onClick={() => openCreator('character')}>
          <Plus size={15} />节点<ChevronDown size={14} className="text-mute" />
        </button>
        <div className="mx-1 h-5 w-px bg-hairline" />
        {creatableKinds.slice(0, 3).map((kind) => {
          const visual = nodeVisuals[kind]
          const Icon = visual.icon
          return <button key={kind} className="grid size-8 place-items-center rounded-sm text-mute hover:bg-hairline-soft hover:text-ink" type="button" title={`新建${visual.label}`} onClick={() => openCreator(kind)}><Icon size={15} /></button>
        })}
      </div>

      {creatingKind !== null ? (
        <section className="absolute top-20 left-space-md z-30 w-72 rounded-sm border border-hairline bg-canvas-elevated shadow-floating">
          <header className="flex h-11 items-center justify-between border-b border-hairline px-space-md">
            <span className="text-label-sm">新建{nodeVisuals[creatingKind].label}</span>
            <button className="grid size-7 place-items-center rounded-sm hover:bg-hairline-soft" type="button" onClick={() => setCreatingKind(null)} title="关闭"><X size={15} /></button>
          </header>
          <div className="space-y-space-sm p-space-md">
            <div className="grid grid-cols-4 gap-1">
              {creatableKinds.map((kind) => {
                const visual = nodeVisuals[kind]
                const Icon = visual.icon
                return <button key={kind} className={`grid h-10 place-items-center rounded-sm border ${creatingKind === kind ? 'border-link bg-link-soft text-link' : 'border-hairline text-mute hover:bg-hairline-soft'}`} type="button" onClick={() => setCreatingKind(kind)} title={visual.label}><Icon size={15} /></button>
              })}
            </div>
            <input className="h-9 w-full rounded-sm border border-hairline bg-canvas px-space-sm outline-none focus:border-link" value={nodeTitle} onChange={(event) => setNodeTitle(event.target.value)} placeholder="标题" />
            <textarea className="min-h-32 w-full resize-y rounded-sm border border-hairline bg-canvas p-space-sm outline-none focus:border-link" value={nodeContent} onChange={(event) => setNodeContent(event.target.value)} placeholder="内容" />
            <button className="flex h-9 w-full items-center justify-center gap-space-xs rounded-sm bg-primary text-button-md text-on-primary disabled:opacity-40" type="button" disabled={createNode.isPending || nodeTitle.trim() === '' || nodeContent.trim() === ''} onClick={createAtViewportCenter}>
              {createNode.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Plus size={15} />}创建
            </button>
          </div>
        </section>
      ) : null}

      <section className="absolute bottom-8 left-1/2 z-20 flex w-[min(calc(100%_-_2rem),48rem)] -translate-x-1/2 items-end gap-space-sm rounded-sm border border-hairline bg-canvas-elevated p-space-sm shadow-floating">
        <textarea className="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-space-xs py-2 outline-none placeholder:text-faint" value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="输入创作指令" />
        <button className="flex h-10 shrink-0 items-center gap-space-xs rounded-sm bg-primary px-space-md text-button-md text-on-primary disabled:opacity-40" type="button" disabled={!canRun || createRun.isPending} onClick={() => model && createRun.mutate({ prompt, contextNodeIds: selectedNodeIds, model }, { onSuccess: (run) => streamRun(run.id) })}>
          {createRun.isPending || activeRunId !== null ? <LoaderCircle className="animate-spin" size={16} /> : <Play size={16} fill="currentColor" />}
          {activeRunId === null ? '生成' : '运行中'}
        </button>
      </section>

      {(events.length > 0 || draft !== '' || streamError !== '') ? (
        <aside className="absolute top-20 right-space-md z-20 flex max-h-[calc(100dvh-15rem)] w-72 flex-col rounded-sm border border-hairline bg-canvas-elevated shadow-floating">
          <header className="flex h-11 shrink-0 items-center justify-between border-b border-hairline px-space-md">
            <span className="flex items-center gap-space-xs text-label-sm"><Sparkles size={15} />Agent</span>
            <span className="font-mono text-body-sm text-mute">{events.length}</span>
          </header>
          <ol className="overflow-y-auto p-space-md">
            {events.map((event) => (
              <li key={event.id} className="relative border-l border-hairline pb-space-md pl-space-md last:pb-0">
                <span className="absolute -left-1 top-1 size-2 rounded-full bg-link" />
                <div className="text-label-sm">{eventLabels[event.type] ?? event.type}</div>
                <div className="mt-1 break-words font-mono text-body-sm text-mute">{eventSummary(event)}</div>
              </li>
            ))}
          </ol>
          {draft !== '' ? <div className="max-h-40 overflow-y-auto border-t border-hairline p-space-md text-body-sm leading-5 text-body">{draft}</div> : null}
          {streamError !== '' ? <div className="border-t border-hairline p-space-sm text-body-sm text-error">{streamError}</div> : null}
        </aside>
      ) : null}
    </main>
  )
}

function isTerminalEvent(type: string) {
  return type === 'run.completed' || type === 'run.failed' || type === 'run.cancelled'
}

function eventSummary(event: AgentEvent) {
  if (event.data === null) return `#${event.sequence}`
  const value = event.data.candidateId ?? event.data.skillId ?? event.data.name ?? event.data.snapshotId
  return typeof value === 'string' ? value : `#${event.sequence}`
}
