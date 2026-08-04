import type { WorkSummary } from '../types/work'

export const recentWorks = [
  {
    id: 'mist-harbor-letters',
    title: '雾港来信',
    updatedLabel: '18 分钟前',
    nodeCount: 24,
    modelName: 'GPT-4.1',
    status: 'draft',
    previewNodes: [
      { id: 'lin', label: '林默', kind: 'character', x: 12, y: 24 },
      { id: 'letter', label: '匿名来信', kind: 'event', x: 52, y: 12 },
      { id: 'chapter-1', label: '第一章', kind: 'chapter-outline', x: 58, y: 58 },
      { id: 'harbor', label: '旧港区', kind: 'location', x: 18, y: 68 },
    ],
    previewEdges: [
      { source: 'lin', target: 'letter' },
      { source: 'letter', target: 'chapter-1' },
      { source: 'harbor', target: 'chapter-1' },
    ],
  },
  {
    id: 'silent-city',
    title: '无声城区',
    updatedLabel: '2 小时前',
    nodeCount: 31,
    modelName: 'Claude Sonnet 4',
    status: 'draft',
    previewNodes: [
      { id: 'reporter', label: '记者周遥', kind: 'character', x: 10, y: 14 },
      { id: 'memory', label: '物品记忆', kind: 'mechanism', x: 58, y: 15 },
      { id: 'accident', label: '被删除的事故', kind: 'event', x: 35, y: 56 },
      { id: 'chapter', label: '失踪档案', kind: 'chapter-outline', x: 68, y: 72 },
    ],
    previewEdges: [
      { source: 'reporter', target: 'accident' },
      { source: 'memory', target: 'accident' },
      { source: 'accident', target: 'chapter' },
    ],
  },
  {
    id: 'mountain-sea-night',
    title: '山海夜航',
    updatedLabel: '昨天',
    nodeCount: 18,
    modelName: 'GPT-4.1',
    status: 'draft',
    previewNodes: [
      { id: 'ship', label: '鲲舟', kind: 'item', x: 12, y: 45 },
      { id: 'captain', label: '船长阿绫', kind: 'character', x: 46, y: 16 },
      { id: 'storm', label: '归墟风暴', kind: 'event', x: 64, y: 54 },
      { id: 'ending', label: '夜航终点', kind: 'chapter-outline', x: 28, y: 74 },
    ],
    previewEdges: [
      { source: 'captain', target: 'ship' },
      { source: 'ship', target: 'storm' },
      { source: 'storm', target: 'ending' },
    ],
  },
  {
    id: 'inverted-city',
    title: '倒悬之城',
    updatedLabel: '3 天前',
    nodeCount: 42,
    modelName: 'Claude Sonnet 4',
    status: 'draft',
    previewNodes: [
      { id: 'city', label: '倒悬城', kind: 'location', x: 10, y: 18 },
      { id: 'architect', label: '建筑师', kind: 'character', x: 58, y: 22 },
      { id: 'gravity', label: '重力停摆', kind: 'mechanism', x: 35, y: 54 },
      { id: 'zero', label: '零层', kind: 'chapter-outline', x: 64, y: 74 },
    ],
    previewEdges: [
      { source: 'city', target: 'gravity' },
      { source: 'architect', target: 'gravity' },
      { source: 'gravity', target: 'zero' },
    ],
  },
  {
    id: 'white-tower',
    title: '白塔纪事',
    updatedLabel: '6 天前',
    nodeCount: 15,
    modelName: 'GPT-4.1',
    status: 'draft',
    previewNodes: [
      { id: 'tower', label: '白塔', kind: 'location', x: 16, y: 20 },
      { id: 'keeper', label: '守塔人', kind: 'character', x: 55, y: 15 },
      { id: 'bell', label: '第十三次钟声', kind: 'event', x: 46, y: 56 },
      { id: 'opening', label: '雪夜', kind: 'chapter-outline', x: 14, y: 72 },
    ],
    previewEdges: [
      { source: 'tower', target: 'keeper' },
      { source: 'keeper', target: 'bell' },
      { source: 'bell', target: 'opening' },
    ],
  },
] satisfies WorkSummary[]
