import { ArrowLeft, LoaderCircle, LockKeyhole, RotateCcw, Save } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'

import { useCanvasNode, useUpdateCanvasNode } from '@/apis/canvas-apis'
import { NodeDocument } from '@/features/canvas/node-detail/NodeDocument'
import { useArchiveLocks } from '@/features/canvas/story-spine/use-archive-locks'

export function NodeEditorPage() {
  const { workId = '', nodeId = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const nodeQuery = useCanvasNode(workId, nodeId)
  const updateNode = useUpdateCanvasNode(workId, nodeId)
  const archiveLocks = useArchiveLocks(workId)
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')

  useEffect(() => {
    if (nodeQuery.data === undefined) return
    setTitle(nodeQuery.data.title)
    setContent(nodeQuery.data.content)
  }, [nodeQuery.data])

  const node = nodeQuery.data
  const isArchiveLocked = archiveLocks.lockedNodeIds.has(nodeId)
  const canEditNode = archiveLocks.isResolved && !isArchiveLocked
  const dirty = node !== undefined && (title !== node.title || content !== node.content)
  const valid = title.trim() !== ''

  const returnToCanvas = () => {
    if (dirty && !window.confirm('当前修改尚未保存，仍要返回画布吗？')) return
    if (isFromCanvas(location.state)) {
      navigate(-1)
      return
    }
    navigate(`/works/${workId}`, { replace: true })
  }

  return (
    <main className="min-h-dvh bg-canvas text-ink">
      <header className="sticky top-0 z-20 flex h-16 items-center justify-between border-b border-hairline bg-canvas-elevated/95 px-space-md backdrop-blur">
        <button
          className="flex h-9 items-center gap-space-xs rounded-sm px-space-sm text-button-md text-mute transition-colors hover:bg-hairline-soft hover:text-ink"
          type="button"
          onClick={returnToCanvas}
        >
          <ArrowLeft size={16} />
          返回画布
        </button>

        <div className="flex items-center gap-space-xs">
          {!canEditNode ? (
            <span className="mr-space-xs inline-flex items-center gap-space-xs text-body-sm text-mute">
              <LockKeyhole aria-hidden="true" size={14} />
              {!archiveLocks.isResolved
                ? archiveLocks.isError ? '无法确认归档状态' : '正在确认归档状态'
                : '已归档锁定'}
            </span>
          ) : null}
          {dirty ? <span className="mr-space-xs text-body-sm text-mute">尚未保存</span> : null}
          <button
            className="flex h-9 items-center gap-space-xs rounded-sm border border-hairline px-space-sm text-button-md text-mute transition-colors hover:bg-hairline-soft hover:text-ink disabled:opacity-40"
            type="button"
            disabled={!canEditNode || !dirty || updateNode.isPending || node === undefined}
            onClick={() => {
              if (node === undefined) return
              setTitle(node.title)
              setContent(node.content)
              updateNode.reset()
            }}
          >
            <RotateCcw size={15} />
            撤销修改
          </button>
          <button
            className="flex h-9 items-center gap-space-xs rounded-sm bg-primary px-space-md text-button-md text-on-primary transition-opacity hover:opacity-85 disabled:opacity-40"
            type="button"
            disabled={!canEditNode || !dirty || !valid || updateNode.isPending || node === undefined}
            onClick={() => {
              if (node === undefined) return
              updateNode.mutate({
                title,
                content,
                expectedRevision: node.revision,
              })
            }}
          >
            {updateNode.isPending
              ? <LoaderCircle className="animate-spin" size={15} />
              : <Save size={15} />}
            保存
          </button>
        </div>
      </header>

      <section className="px-space-xl py-space-2xl">
        {nodeQuery.isPending ? (
          <EditorStatus message="正在读取节点内容" />
        ) : nodeQuery.isError ? (
          <EditorStatus
            message={nodeQuery.error instanceof Error ? nodeQuery.error.message : '节点内容读取失败'}
            tone="error"
          />
        ) : node !== undefined ? (
          <>
            {updateNode.isError ? (
              <div className="mx-auto mb-space-lg flex max-w-3xl items-center justify-between gap-space-md rounded-sm border border-error/25 bg-error/5 px-space-md py-space-sm text-body-sm text-error">
                <span>{updateNode.error instanceof Error ? updateNode.error.message : '节点保存失败'}</span>
                <button
                  className="shrink-0 underline underline-offset-2"
                  type="button"
                  onClick={() => {
                    updateNode.reset()
                    void nodeQuery.refetch()
                  }}
                >
                  重新加载
                </button>
              </div>
            ) : null}
            <NodeDocument
              node={node}
              mode={canEditNode ? 'edit' : 'read'}
              title={title}
              content={content}
              onTitleChange={setTitle}
              onContentChange={setContent}
            />
          </>
        ) : null}
      </section>
    </main>
  )
}

function isFromCanvas(state: unknown): state is { fromCanvas: true } {
  return typeof state === 'object' && state !== null && 'fromCanvas' in state && state.fromCanvas === true
}

function EditorStatus({ message, tone = 'muted' }: { message: string; tone?: 'muted' | 'error' }) {
  return (
    <div className={`grid min-h-[70dvh] place-items-center text-body-md ${tone === 'error' ? 'text-error' : 'text-mute'}`}>
      {message}
    </div>
  )
}
