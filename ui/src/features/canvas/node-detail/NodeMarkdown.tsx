import { memo } from 'react'

import { MessageResponse } from '@/components/ai-elements/message'

interface NodeMarkdownProps {
  children: string
}

const documentClassName = [
  'text-body-lg leading-8 text-body',
  '[&_p]:my-space-md [&_p:first-child]:mt-0',
  '[&_h1]:mt-space-xl [&_h1]:mb-space-sm [&_h1]:text-heading-md',
  '[&_h2]:mt-space-xl [&_h2]:mb-space-sm [&_h2]:text-heading-md',
  '[&_h3]:mt-space-lg [&_h3]:mb-space-xs [&_h3]:text-label-sm',
  '[&_ul]:my-space-md [&_ol]:my-space-md',
  '[&_blockquote]:my-space-lg',
].join(' ')

export const NodeMarkdown = memo(function NodeMarkdown({ children }: NodeMarkdownProps) {
  return (
    <MessageResponse className={documentClassName}>
      {children}
    </MessageResponse>
  )
})
