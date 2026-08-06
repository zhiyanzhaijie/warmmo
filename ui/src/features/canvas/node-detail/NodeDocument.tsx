import { useMemo } from 'react'

import { nodeDefinitions } from '@/features/canvas/nodes/definitions'
import { NodeMarkdown } from '@/features/canvas/node-detail/NodeMarkdown'
import type { CanvasNode } from '@/types/canvas'

const wordCountFormatter = new Intl.NumberFormat('zh-CN')
const whitespaceCharacter = /\s/u

interface NodeDocumentProps {
  node: CanvasNode
  mode: 'read' | 'edit'
  title?: string
  content?: string
  onTitleChange?: (value: string) => void
  onContentChange?: (value: string) => void
}

export function NodeDocument({
  node,
  mode,
  title = node.title,
  content = node.content,
  onTitleChange,
  onContentChange,
}: NodeDocumentProps) {
  const definition = nodeDefinitions[node.kind]
  const Icon = definition.icon
  const wordCount = useMemo(
    () => node.kind === 'chapter-section' && mode === 'read' ? countNonWhitespaceCharacters(content) : null,
    [content, mode, node.kind],
  )

  return (
    <article className="mx-auto w-full max-w-3xl">
      <div className="flex items-center gap-space-xs font-mono text-body-sm text-mute">
        <Icon size={14} />
        <span>{definition.label}</span>
        <span aria-hidden="true">·</span>
        <span>REV {node.revision}</span>
        {wordCount !== null ? (
          <>
            <span aria-hidden="true">·</span>
            <span>{wordCountFormatter.format(wordCount)} 字</span>
          </>
        ) : null}
      </div>

      {mode === 'edit' ? (
        <input
          aria-label="节点标题"
          className="mt-space-md w-full border-0 bg-transparent text-heading-lg text-ink outline-none placeholder:text-faint"
          value={title}
          onChange={(event) => onTitleChange?.(event.target.value)}
          placeholder="节点标题"
        />
      ) : (
        <h1 className="mt-space-md text-heading-lg">{node.title}</h1>
      )}

      <div className="mt-space-xl border-t border-hairline pt-space-xl">
        {mode === 'edit' ? (
          <textarea
            aria-label="节点正文"
            className="min-h-[60dvh] w-full resize-none border-0 bg-transparent text-body-lg leading-8 text-body outline-none placeholder:text-faint"
            value={content}
            onChange={(event) => onContentChange?.(event.target.value)}
            placeholder="输入节点正文"
          />
        ) : (
          <NodeMarkdown>{node.content}</NodeMarkdown>
        )}
      </div>
    </article>
  )
}

function countNonWhitespaceCharacters(content: string) {
  let count = 0
  for (const character of content) {
    if (!whitespaceCharacter.test(character)) count += 1
  }
  return count
}
