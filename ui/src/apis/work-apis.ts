import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { coreClient } from '@/lib/api/core-client'
import type { CreateWorkInput, UpdateWorkInput, WorkDetail, WorkFolder, WorkSummary } from '@/types/work'

interface WorksResponse {
  works: WorkSummary[]
}

interface WorkFoldersResponse {
  folders: WorkFolder[]
}

export const workKeys = {
  all: ['works'] as const,
  list: () => [...workKeys.all, 'list'] as const,
  detail: (workId: string) => [...workKeys.all, 'detail', workId] as const,
  folders: () => [...workKeys.all, 'folders'] as const,
}

export function useWorks() {
  return useQuery({
    queryKey: workKeys.list(),
    queryFn: async ({ signal }) => {
      const response = await coreClient<WorksResponse>('/works', { signal })
      return response.works
    },
    retry: false,
  })
}

export function useWork(workId: string) {
  const queryClient = useQueryClient()
  return useQuery({
    queryKey: workKeys.detail(workId),
    queryFn: ({ signal }) => coreClient<WorkDetail>(`/works/${encodeURIComponent(workId)}`, { signal }),
    placeholderData: () => {
      const summary = queryClient.getQueryData<WorkSummary[]>(workKeys.list())
        ?.find((work) => work.id === workId)
      if (summary === undefined) return undefined
      return {
        id: summary.id,
        title: summary.title,
        description: summary.description,
        folderId: summary.folderId,
        folderName: summary.folderName,
        status: summary.status,
        revision: summary.revision,
        updatedAt: summary.updatedAt,
      }
    },
  })
}

export function useCreateWork() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateWorkInput) => coreClient<WorkSummary>('/works', {
      method: 'POST',
      body: input,
    }),
    onSuccess: (work) => {
      queryClient.setQueryData<WorkSummary[]>(workKeys.list(), (works = []) => [work, ...works])
      queryClient.setQueryData<WorkDetail>(workKeys.detail(work.id), {
        id: work.id,
        title: work.title,
        description: work.description,
        folderId: work.folderId,
        folderName: work.folderName,
        status: work.status,
        revision: work.revision,
        updatedAt: work.updatedAt,
      })
    },
  })
}

export function useUpdateWork() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: UpdateWorkInput) => coreClient<WorkDetail>(`/works/${encodeURIComponent(input.id)}`, {
      method: 'PATCH',
      body: {
        title: input.title,
        description: input.description,
        folderId: input.folderId,
        status: input.status,
        expectedRevision: input.expectedRevision,
      },
    }),
    onSuccess: (work) => {
      queryClient.setQueryData(workKeys.detail(work.id), work)
      void queryClient.invalidateQueries({ queryKey: workKeys.list() })
    },
  })
}

export function useDeleteWork() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (workId: string) => {
      await coreClient(`/works/${encodeURIComponent(workId)}`, {
        method: 'DELETE',
        responseType: 'text',
      })
    },
    onSuccess: (_, workId) => {
      queryClient.setQueryData<WorkSummary[]>(workKeys.list(), (works = []) =>
        works.filter((work) => work.id !== workId))
      queryClient.removeQueries({ queryKey: workKeys.detail(workId) })
      queryClient.removeQueries({ queryKey: ['canvas', workId] })
      queryClient.removeQueries({ queryKey: ['chapter-archives', workId] })
    },
  })
}

export function useWorkFolders(enabled = true) {
  return useQuery({
    queryKey: workKeys.folders(),
    queryFn: async ({ signal }) => {
      const response = await coreClient<WorkFoldersResponse>('/work-folders', { signal })
      return response.folders
    },
    enabled,
  })
}

export function useCreateWorkFolder() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => coreClient<WorkFolder>('/work-folders', { method: 'POST', body: { name } }),
    onSuccess: (folder) => {
      queryClient.setQueryData<WorkFolder[]>(workKeys.folders(), (folders = []) => [...folders, folder])
    },
  })
}
