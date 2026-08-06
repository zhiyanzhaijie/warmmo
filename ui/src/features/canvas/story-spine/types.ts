export type StorySpineTone = 'cyan' | 'magenta' | 'amber' | 'violet' | 'green'

export interface StorySpineSection {
  id: string
  ordinal: number
  title: string
  summary: string
  nodeId: string
  nodeRevision: number
  isNodeAvailable: boolean
}

export interface StorySpineChapter {
  archiveId: string
  archiveRevision: number
  title: string
  summary: string
  nodeId: string
  isNodeAvailable: boolean
  isProjectionPending: boolean
  tone: StorySpineTone
  sections: StorySpineSection[]
}
