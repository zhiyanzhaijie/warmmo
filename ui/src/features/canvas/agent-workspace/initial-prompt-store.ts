import { create } from 'zustand'

// 首页创建作品后携带的初始 prompt：写入方在跳转前 set，画布内对应作品的消费方取走即清。
interface InitialPromptState {
  initialPrompt?: { workId: string; prompt: string }
  setInitialPrompt: (workId: string, prompt: string) => void
  takeInitialPrompt: (workId: string) => string | undefined
}

export const useInitialPromptStore = create<InitialPromptState>((set, get) => ({
  initialPrompt: undefined,
  setInitialPrompt: (workId, prompt) => set({ initialPrompt: { workId, prompt } }),
  takeInitialPrompt: (workId) => {
    const pending = get().initialPrompt
    if (pending === undefined || pending.workId !== workId) return undefined
    set({ initialPrompt: undefined })
    return pending.prompt
  },
}))
