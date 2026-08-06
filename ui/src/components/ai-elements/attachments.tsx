import type { FileUIPart, SourceDocumentUIPart } from 'ai'
import {
  FileTextIcon,
  GlobeIcon,
  ImageIcon,
  Music2Icon,
  PaperclipIcon,
  VideoIcon,
  XIcon,
} from 'lucide-react'
import {
  createContext,
  useContext,
  type ComponentProps,
  type HTMLAttributes,
  type ReactNode,
} from 'react'

import { Button } from '@/components/ui/button'
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card'
import { cn } from '@/lib/utils'

export type AttachmentData = (FileUIPart | SourceDocumentUIPart) & { id: string }
type AttachmentVariant = 'grid' | 'inline' | 'list'
type MediaCategory = 'audio' | 'document' | 'image' | 'source' | 'unknown' | 'video'

interface AttachmentsContextValue {
  variant: AttachmentVariant
}

interface AttachmentContextValue extends AttachmentsContextValue {
  data: AttachmentData
  mediaCategory: MediaCategory
  onRemove?: () => void
}

const AttachmentsContext = createContext<AttachmentsContextValue | null>(null)
const AttachmentContext = createContext<AttachmentContextValue | null>(null)

function useAttachmentsContext() {
  const context = useContext(AttachmentsContext)
  if (context === null) throw new Error('Attachment must be used within Attachments')
  return context
}

function useAttachmentContext() {
  const context = useContext(AttachmentContext)
  if (context === null) throw new Error('Attachment children must be used within Attachment')
  return context
}

export interface AttachmentsProps extends HTMLAttributes<HTMLDivElement> {
  variant?: AttachmentVariant
}

export function Attachments({
  variant = 'grid',
  className,
  children,
  ...props
}: AttachmentsProps) {
  return (
    <AttachmentsContext.Provider value={{ variant }}>
      <div
        className={cn(
          'flex items-start',
          variant === 'list' ? 'flex-col gap-2' : 'flex-wrap gap-2',
          variant === 'grid' && 'ml-auto w-fit',
          className,
        )}
        {...props}
      >
        {children}
      </div>
    </AttachmentsContext.Provider>
  )
}

export interface AttachmentProps extends HTMLAttributes<HTMLDivElement> {
  data: AttachmentData
  onRemove?: () => void
}

export function Attachment({
  data,
  onRemove,
  className,
  children,
  ...props
}: AttachmentProps) {
  const { variant } = useAttachmentsContext()
  const mediaCategory = getMediaCategory(data)
  return (
    <AttachmentContext.Provider value={{ data, mediaCategory, onRemove, variant }}>
      <div
        className={cn(
          'group relative',
          variant === 'grid' && 'size-24 overflow-hidden rounded-md',
          variant === 'inline' && 'flex h-8 cursor-default select-none items-center gap-1.5 rounded-sm border border-hairline px-1.5 text-body-sm font-medium transition-colors hover:bg-hairline-soft',
          variant === 'list' && 'flex w-full items-center gap-3 rounded-sm border border-hairline p-3 hover:bg-hairline-soft',
          className,
        )}
        {...props}
      >
        {children}
      </div>
    </AttachmentContext.Provider>
  )
}

export interface AttachmentPreviewProps extends HTMLAttributes<HTMLDivElement> {
  fallbackIcon?: ReactNode
}

export function AttachmentPreview({ fallbackIcon, className, ...props }: AttachmentPreviewProps) {
  const { data, mediaCategory, variant } = useAttachmentContext()
  const iconClassName = variant === 'inline' ? 'size-3' : 'size-4'
  let preview: ReactNode = fallbackIcon
  if (preview === undefined) {
    if (mediaCategory === 'image' && data.type === 'file' && data.url !== '') {
      preview = <img alt={data.filename ?? '附件预览'} className="size-full object-cover" src={data.url} />
    } else {
      const Icon = mediaCategoryIcons[mediaCategory]
      preview = <Icon aria-hidden="true" className={cn(iconClassName, 'text-mute')} />
    }
  }
  return (
    <div
      className={cn(
        'flex shrink-0 items-center justify-center overflow-hidden',
        variant === 'grid' && 'size-full bg-hairline-soft',
        variant === 'inline' && 'size-5 rounded-sm bg-canvas',
        variant === 'list' && 'size-12 rounded-sm bg-hairline-soft',
        className,
      )}
      {...props}
    >
      {preview}
    </div>
  )
}

export interface AttachmentInfoProps extends HTMLAttributes<HTMLDivElement> {
  showMediaType?: boolean
}

export function AttachmentInfo({ showMediaType = false, className, ...props }: AttachmentInfoProps) {
  const { data, variant } = useAttachmentContext()
  if (variant === 'grid') return null
  return (
    <div className={cn('min-w-0 flex-1', className)} {...props}>
      <span className="block truncate">{getAttachmentLabel(data)}</span>
      {showMediaType ? <span className="block truncate text-body-sm text-mute">{data.mediaType}</span> : null}
    </div>
  )
}

export interface AttachmentRemoveProps extends ComponentProps<typeof Button> {
  label?: string
}

export function AttachmentRemove({
  label = '移除附件',
  className,
  children,
  onClick,
  ...props
}: AttachmentRemoveProps) {
  const { onRemove, variant } = useAttachmentContext()
  if (onRemove === undefined) return null
  return (
    <Button
      aria-label={label}
      className={cn(
        variant === 'grid' && 'absolute right-2 top-2 size-6 rounded-full bg-canvas/80 p-0 opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100 [&>svg]:size-3',
        variant === 'inline' && 'size-5 shrink-0 rounded-sm p-0 text-mute opacity-0 transition-[color,opacity] group-hover:opacity-100 group-focus-within:opacity-100 hover:text-ink [&>svg]:size-2.5',
        variant === 'list' && 'size-8 shrink-0 rounded-sm p-0 [&>svg]:size-4',
        className,
      )}
      type="button"
      variant="ghost"
      onClick={(event) => {
        event.stopPropagation()
        onRemove()
        onClick?.(event)
      }}
      {...props}
    >
      {children ?? <XIcon />}
      <span className="sr-only">{label}</span>
    </Button>
  )
}

export type AttachmentHoverCardProps = ComponentProps<typeof HoverCard>
export function AttachmentHoverCard(props: AttachmentHoverCardProps) {
  return <HoverCard openDelay={180} closeDelay={80} {...props} />
}

export type AttachmentHoverCardTriggerProps = ComponentProps<typeof HoverCardTrigger>
export function AttachmentHoverCardTrigger(props: AttachmentHoverCardTriggerProps) {
  return <HoverCardTrigger {...props} />
}

export type AttachmentHoverCardContentProps = ComponentProps<typeof HoverCardContent>
export function AttachmentHoverCardContent({ className, ...props }: AttachmentHoverCardContentProps) {
  return <HoverCardContent align="start" className={cn('w-64 rounded-sm border-0 p-space-sm shadow-floating', className)} {...props} />
}

export type AttachmentEmptyProps = HTMLAttributes<HTMLDivElement>
export function AttachmentEmpty({ className, ...props }: AttachmentEmptyProps) {
  return <div className={cn('py-space-sm text-center text-body-sm text-mute', className)} {...props} />
}

const mediaCategoryIcons = {
  audio: Music2Icon,
  document: FileTextIcon,
  image: ImageIcon,
  source: GlobeIcon,
  unknown: PaperclipIcon,
  video: VideoIcon,
} satisfies Record<MediaCategory, typeof ImageIcon>

function getMediaCategory(data: AttachmentData): MediaCategory {
  if (data.type === 'source-document') return 'source'
  const mediaType = data.mediaType.toLocaleLowerCase()
  if (mediaType.startsWith('image')) return 'image'
  if (mediaType.startsWith('video')) return 'video'
  if (mediaType.startsWith('audio')) return 'audio'
  if (mediaType.includes('pdf') || mediaType.startsWith('text')) return 'document'
  return 'unknown'
}

function getAttachmentLabel(data: AttachmentData) {
  return data.type === 'source-document' ? data.title : data.filename ?? '附件'
}
