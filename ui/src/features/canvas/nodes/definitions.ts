import {
  BookOpenText,
  Castle,
  Clock3,
  Cog,
  FilePenLine,
  Gem,
  ListOrdered,
  ListTree,
  MapPinned,
  Milestone,
  UserRound,
  type LucideIcon,
} from 'lucide-react'

export const nodeCategoryDefinitions = {
  entity: {
    label: '故事实体',
    description: '故事中可以被引用、参与事件或建立关系的人、物、地点与时间。',
  },
  structure: {
    label: '故事结构',
    description: '定义故事得以成立和展开的世界、机制与事件。',
  },
  asset: {
    label: '写作资产',
    description: '由故事实体和结构逐步生成的章节概览、子章节规划、正文与完稿。',
  },
} as const

export type CanvasNodeCategory = keyof typeof nodeCategoryDefinitions
export type CanvasNodeCreationMode = 'manual' | 'derived'
export type CanvasNodeShortcut = 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8

interface CanvasNodeDefinitionBase {
  category: CanvasNodeCategory
  label: string
  description: string
  icon: LucideIcon
  accentClassName: string
  accentColor: string
}

export type CanvasNodeDefinition = CanvasNodeDefinitionBase & (
  | { creationMode: 'manual'; shortcut: CanvasNodeShortcut }
  | { creationMode: 'derived'; shortcut?: never }
)

function defineCanvasNodes<const T extends Record<string, CanvasNodeDefinition>>(definitions: T) {
  return definitions
}

export const nodeDefinitions = defineCanvasNodes({
  character: {
    category: 'entity',
    label: '角色',
    description: '拥有身份、目标、关系和变化轨迹的故事参与者。',
    icon: UserRound,
    accentClassName: 'bg-link',
    accentColor: 'var(--color-link)',
    creationMode: 'manual',
    shortcut: 1,
  },
  item: {
    category: 'entity',
    label: '物品',
    description: '可被持有、使用、交换或影响事件的故事实体。',
    icon: Gem,
    accentClassName: 'bg-warning',
    accentColor: 'var(--color-warning)',
    creationMode: 'manual',
    shortcut: 2,
  },
  location: {
    category: 'entity',
    label: '地点',
    description: '角色活动和事件发生的空间实体，可作为创作上下文被引用。',
    icon: MapPinned,
    accentClassName: 'bg-cyan',
    accentColor: 'var(--color-cyan)',
    creationMode: 'manual',
    shortcut: 3,
  },
  time: {
    category: 'entity',
    label: '时间',
    description: '可被多个事件复用的时代、时期或关键时间点。',
    icon: Clock3,
    accentClassName: 'bg-pink',
    accentColor: 'var(--color-pink)',
    creationMode: 'manual',
    shortcut: 4,
  },
  world: {
    category: 'structure',
    label: '世界观',
    description: '故事发生的总体场域；通过层级关系组织根世界与领域世界观。',
    icon: Castle,
    accentClassName: 'bg-magenta',
    accentColor: 'var(--color-magenta)',
    creationMode: 'manual',
    shortcut: 5,
  },
  mechanism: {
    category: 'structure',
    label: '机制',
    description: '能力、规则、系统及其代价、限制和作用范围。',
    icon: Cog,
    accentClassName: 'bg-violet',
    accentColor: 'var(--color-violet)',
    creationMode: 'manual',
    shortcut: 6,
  },
  event: {
    category: 'structure',
    label: '事件',
    description: '由人物、地点、时间和因果关系共同构成的故事变化。',
    icon: Milestone,
    accentClassName: 'bg-error',
    accentColor: 'var(--color-error)',
    creationMode: 'manual',
    shortcut: 7,
  },
  'chapter-outline': {
    category: 'asset',
    label: '章节概览',
    description: '组织章节目标、冲突、事件顺序和预期结果的写作资产。',
    icon: ListTree,
    accentClassName: 'bg-ink',
    accentColor: 'var(--color-ink)',
    creationMode: 'manual',
    shortcut: 8,
  },
  'section-outline': {
    category: 'asset',
    label: '子章节规划',
    description: '从章节概览拆分出的写作规划，定义目标、节拍、边界状态和篇幅。',
    icon: ListOrdered,
    accentClassName: 'bg-mute',
    accentColor: 'var(--color-mute)',
    creationMode: 'derived',
  },
  'chapter-section': {
    category: 'asset',
    label: '章节小节',
    description: '根据子章节规划生成并可持续修改的完整正文单元。',
    icon: FilePenLine,
    accentClassName: 'bg-primary',
    accentColor: 'var(--color-primary)',
    creationMode: 'derived',
  },
  manuscript: {
    category: 'asset',
    label: '完稿',
    description: '由章节小节组织、润色、校对并确认后的最终写作资产。',
    icon: BookOpenText,
    accentClassName: 'bg-ink',
    accentColor: 'var(--color-ink)',
    creationMode: 'derived',
  },
})

export type CanvasNodeKind = keyof typeof nodeDefinitions

export const canvasNodeKinds = Object.keys(nodeDefinitions) as CanvasNodeKind[]

export const creatableNodeKinds = canvasNodeKinds.filter(
  (kind) => nodeDefinitions[kind].creationMode === 'manual',
)

export const derivedNodeKinds = canvasNodeKinds.filter(
  (kind) => nodeDefinitions[kind].creationMode === 'derived',
)

export function getNodeKindsByCategory(category: CanvasNodeCategory) {
  return canvasNodeKinds.filter((kind) => nodeDefinitions[kind].category === category)
}
