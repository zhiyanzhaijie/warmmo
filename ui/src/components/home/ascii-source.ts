import { useMemo } from 'react'

import { useCanvasNodes } from '@/apis/canvas-apis'
import { useWorks } from '@/apis/work-apis'

import defaultManuscript from './ascii-default.md?raw'

const MAX_SOURCE_CHARS = 8000

export function useAsciiSource(): string {
  const works = useWorks()
  const firstWorkId = works.data?.find((work) => work.status === 'active')?.id ?? ''
  const nodes = useCanvasNodes(firstWorkId, firstWorkId !== '')

  return useMemo(() => {
    const real = stripMarkdown(
      (nodes.data ?? [])
        .filter((node) => node.kind === 'chapter-section' && node.content.trim() !== '')
        .map((node) => node.content)
        .join('\n\n'),
    ).trim()
    if (real !== '') return real.slice(0, MAX_SOURCE_CHARS)
    return stripMarkdown(defaultManuscript)
  }, [nodes.data])
}

export function stripMarkdown(input: string): string {
  return input
    .replace(/\r\n?/g, '\n')
    .replace(/^---\n[\s\S]*?\n---\n/, '')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, '')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s{0,3}>\s?/gm, '')
    .replace(/^\s*[-*+]\s+/gm, '')
    .replace(/^\s*\d+\.\s+/gm, '')
    .replace(/(\*\*|__)([\s\S]*?)\1/g, '$2')
    .replace(/(\*|_)([^*_]*)\1/g, '$2')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}
