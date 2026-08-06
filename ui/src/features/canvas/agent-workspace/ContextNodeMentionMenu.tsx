import {
  PromptInputCommand,
  PromptInputCommandEmpty,
  PromptInputCommandGroup,
  PromptInputCommandItem,
  PromptInputCommandList,
} from '@/components/ai-elements/prompt-input'
import type { CanvasContextNode } from '@/features/canvas/agent-workspace/types'
import { creatableNodeKinds, nodeDefinitions, type CanvasNodeShortcut } from '@/features/canvas/nodes/definitions'
import { cn } from '@/lib/utils'

export type ContextNodeMentionOption =
  | { id: string; kind: 'shortcut'; shortcut: CanvasNodeShortcut; nodeCount: number }
  | { id: string; kind: 'node'; node: CanvasContextNode }

export interface ContextNodeMentionMenuModel {
  heading: string
  mode: 'nodes' | 'shortcuts'
  options: ContextNodeMentionOption[]
}

interface CanvasContextNodeMentionMenuProps {
  disabled: boolean
  model: ContextNodeMentionMenuModel
  selectedOptionId: string
  onSelectedOptionChange: (optionId: string) => void
  onSelect: (option: ContextNodeMentionOption) => void
}

export function getContextNodeMentionMenuModel(
  nodes: CanvasContextNode[],
  query: string,
): ContextNodeMentionMenuModel {
  const shortcut = getNodeShortcut(query)
  if (shortcut !== null) {
    const nodeKind = creatableNodeKinds.find((kind) => nodeDefinitions[kind].shortcut === shortcut)
    if (nodeKind === undefined) return { heading: `@${shortcut}`, mode: 'nodes', options: [] }
    const definition = nodeDefinitions[nodeKind]
    const titleQuery = query.slice(1).trim().toLocaleLowerCase()
    const matchingNodes = nodes.filter((node) => node.kind === nodeKind && (
      titleQuery === ''
      || node.title.toLocaleLowerCase().includes(titleQuery)
      || node.content.toLocaleLowerCase().includes(titleQuery)
    ))
    return {
      heading: `@${shortcut} ${definition.label}`,
      mode: 'nodes',
      options: matchingNodes.map((node) => ({ id: `node:${node.id}`, kind: 'node', node })),
    }
  }

  const normalizedQuery = query.trim().toLocaleLowerCase()
  const nodeCountByKind = new Map<string, number>()
  for (const node of nodes) {
    nodeCountByKind.set(node.kind, (nodeCountByKind.get(node.kind) ?? 0) + 1)
  }
  const options = creatableNodeKinds.flatMap((nodeKind) => {
    const definition = nodeDefinitions[nodeKind]
    if (definition.creationMode !== 'manual') return []
    const nodeCount = nodeCountByKind.get(nodeKind) ?? 0
    if (
      normalizedQuery !== ''
      && !definition.label.toLocaleLowerCase().includes(normalizedQuery)
      && !String(definition.shortcut).includes(normalizedQuery)
    ) return []
    return [{
      id: `shortcut:${definition.shortcut}`,
      kind: 'shortcut' as const,
      shortcut: definition.shortcut,
      nodeCount,
    }]
  })
  return { heading: '', mode: 'shortcuts', options }
}

export function CanvasContextNodeMentionMenu({
  disabled,
  model,
  selectedOptionId,
  onSelectedOptionChange,
  onSelect,
}: CanvasContextNodeMentionMenuProps) {
  return (
    <PromptInputCommand
      aria-label="引用画布节点"
      className={cn(
        'absolute bottom-[calc(100%_+_0.375rem)] left-space-md z-30 h-auto max-h-56 rounded-sm border border-hairline bg-canvas-elevated text-ink shadow-floating',
        model.mode === 'shortcuts'
          ? 'w-[min(13rem,calc(100%_-_2rem))]'
          : 'w-[min(18rem,calc(100%_-_2rem))]',
      )}
      shouldFilter={false}
      value={selectedOptionId}
      onValueChange={onSelectedOptionChange}
      onMouseDown={(event) => event.preventDefault()}
    >
      <PromptInputCommandList className="max-h-56 p-space-xxs">
        <PromptInputCommandEmpty className="py-space-md text-body-sm text-mute">
          没有匹配的节点
        </PromptInputCommandEmpty>
        <PromptInputCommandGroup
          heading={model.heading}
          className={cn(
            'p-0 [&_[cmdk-group-heading]]:px-space-sm [&_[cmdk-group-heading]]:py-space-xs [&_[cmdk-group-heading]]:font-mono [&_[cmdk-group-heading]]:text-mono-eyebrow [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:text-mute',
            model.mode === 'shortcuts' && '[&_[cmdk-group-heading]]:hidden [&_[cmdk-group-items]]:grid [&_[cmdk-group-items]]:grid-cols-4 [&_[cmdk-group-items]]:gap-space-xxs',
          )}
        >
          {model.options.map((option) => option.kind === 'shortcut'
            ? <ShortcutOption key={option.id} disabled={disabled} option={option} onSelect={onSelect} />
            : <NodeOption key={option.id} disabled={disabled} option={option} onSelect={onSelect} />)}
        </PromptInputCommandGroup>
      </PromptInputCommandList>
    </PromptInputCommand>
  )
}

function ShortcutOption({
  disabled,
  option,
  onSelect,
}: {
  disabled: boolean
  option: Extract<ContextNodeMentionOption, { kind: 'shortcut' }>
  onSelect: (option: ContextNodeMentionOption) => void
}) {
  const nodeKind = creatableNodeKinds.find((kind) => nodeDefinitions[kind].shortcut === option.shortcut)
  if (nodeKind === undefined) return null
  const definition = nodeDefinitions[nodeKind]
  if (definition.creationMode !== 'manual') return null
  const NodeIcon = definition.icon
  return (
    <PromptInputCommandItem
      aria-label={`${option.shortcut} ${definition.label}，${option.nodeCount} 个节点`}
      disabled={disabled}
      value={option.id}
      className="relative grid size-10 place-items-center rounded-sm p-0 data-[selected=true]:bg-hairline-soft data-[selected=true]:text-ink"
      onSelect={() => onSelect(option)}
    >
      <NodeIcon aria-hidden="true" size={16} style={{ color: definition.accentColor }} />
      <kbd className="absolute bottom-0.5 right-1 font-mono text-[0.5625rem] leading-none text-faint">
        {option.shortcut}
      </kbd>
    </PromptInputCommandItem>
  )
}

function NodeOption({
  disabled,
  option,
  onSelect,
}: {
  disabled: boolean
  option: Extract<ContextNodeMentionOption, { kind: 'node' }>
  onSelect: (option: ContextNodeMentionOption) => void
}) {
  const definition = nodeDefinitions[option.node.kind]
  const NodeIcon = definition.icon
  return (
    <PromptInputCommandItem
      disabled={disabled}
      value={option.id}
      className="h-9 gap-space-xs rounded-sm px-space-xs text-body-sm data-[selected=true]:bg-hairline-soft data-[selected=true]:text-ink"
      onSelect={() => onSelect(option)}
    >
      <span
        aria-hidden="true"
        className="grid size-6 shrink-0 place-items-center rounded-full bg-hairline-soft"
        style={{ color: definition.accentColor }}
      >
        <NodeIcon size={13} />
      </span>
      <span className="min-w-0 flex-1 truncate">{option.node.title}</span>
    </PromptInputCommandItem>
  )
}

function getNodeShortcut(query: string): CanvasNodeShortcut | null {
  const candidate = Number(query[0])
  return Number.isInteger(candidate) && candidate >= 1 && candidate <= 8
    ? candidate as CanvasNodeShortcut
    : null
}
