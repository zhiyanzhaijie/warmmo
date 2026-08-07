import type { Editor, JSONContent, Range } from '@tiptap/core'
import { Document } from '@tiptap/extension-document'
import { HardBreak } from '@tiptap/extension-hard-break'
import { History } from '@tiptap/extension-history'
import { Mention } from '@tiptap/extension-mention'
import { Paragraph } from '@tiptap/extension-paragraph'
import { Text } from '@tiptap/extension-text'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import { EditorContent, useEditor } from '@tiptap/react'
import { exitSuggestion } from '@tiptap/suggestion'
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'

import {
  CanvasContextNodeMentionMenu,
  getContextNodeMentionMenuModel,
  type ContextNodeMentionMenuModel,
  type ContextNodeMentionOption,
} from '@/features/canvas/agent-workspace/ContextNodeMentionMenu'
import type { CanvasContextNode } from '@/features/canvas/agent-workspace/types'
import { nodeDefinitions } from '@/features/canvas/nodes/definitions'

export interface CanvasPromptValue {
  contextNodeIds: string[]
  displayText: string
  document: JSONContent
  requestText: string
}

export function createCanvasPromptValueFromText(text: string): CanvasPromptValue {
  return {
    contextNodeIds: [],
    displayText: text,
    document: {
      type: 'doc',
      content: text.split('\n').map((line) => ({
        type: 'paragraph',
        ...(line === '' ? {} : { content: [{ type: 'text', text: line }] }),
      })),
    },
    requestText: text,
  }
}

export interface CanvasPromptEditorHandle {
  removeContextNode: (nodeId: string) => void
}

interface CanvasPromptEditorProps {
  availableContextNodes: CanvasContextNode[]
  disabled: boolean
  initialValue: CanvasPromptValue
  placeholder: string
  onChange: (value: CanvasPromptValue) => void
}

interface MentionSuggestionState {
  command: (option: ContextNodeMentionOption) => void
  query: string
}

interface CanvasPromptMentionMenuHandle {
  onKeyDown: (event: KeyboardEvent) => boolean
}

interface CanvasPromptMentionMenuProps {
  disabled: boolean
  model: ContextNodeMentionMenuModel
  onSelect: (option: ContextNodeMentionOption) => void
}

export const CanvasPromptEditor = forwardRef<CanvasPromptEditorHandle, CanvasPromptEditorProps>(function CanvasPromptEditor({
  availableContextNodes,
  disabled,
  initialValue,
  placeholder,
  onChange,
}, ref) {
  const availableContextNodesRef = useRef(availableContextNodes)
  const mentionMenuOpenRef = useRef(false)
  const mentionMenuRef = useRef<CanvasPromptMentionMenuHandle>(null)
  const onChangeRef = useRef(onChange)
  const promptValueRef = useRef(initialValue)
  const [isEmpty, setIsEmpty] = useState(initialValue.displayText.trim() === '')
  const [suggestion, setSuggestion] = useState<MentionSuggestionState | null>(null)

  availableContextNodesRef.current = availableContextNodes
  onChangeRef.current = onChange

  const emitChange = (nextEditor: Editor) => {
    const value = getCanvasPromptValue(nextEditor)
    promptValueRef.current = value
    setIsEmpty(value.displayText.trim() === '')
    onChangeRef.current(value)
  }

  const editor = useEditor({
    immediatelyRender: true,
    shouldRerenderOnTransaction: false,
    content: initialValue.document,
    extensions: [
      Document,
      Paragraph,
      Text,
      HardBreak,
      History,
      Mention.configure({
        HTMLAttributes: { class: 'canvas-prompt-mention' },
        deleteTriggerWithBackspace: true,
        suggestion: {
          allowedPrefixes: null,
          items: ({ query }) => getContextNodeMentionMenuModel(availableContextNodesRef.current, query).options,
          command: ({ editor: nextEditor, range, props }: {
            editor: Editor
            range: Range
            props: ContextNodeMentionOption
          }) => {
            if (props.kind === 'shortcut') {
              nextEditor.chain().focus().insertContentAt(range, `@${props.shortcut}`).run()
              return
            }
            const definition = nodeDefinitions[props.node.kind]
            nextEditor.chain().focus().insertContentAt(range, [
              {
                type: 'mention',
                attrs: {
                  id: props.node.id,
                  label: `${definition.label}：${props.node.title}`,
                },
              },
              { type: 'text', text: ' ' },
            ]).run()
          },
          render: () => ({
            onStart: (props) => {
              mentionMenuOpenRef.current = true
              setSuggestion({
                command: props.command as (option: ContextNodeMentionOption) => void,
                query: props.query,
              })
            },
            onUpdate: (props) => {
              mentionMenuOpenRef.current = true
              setSuggestion({
                command: props.command as (option: ContextNodeMentionOption) => void,
                query: props.query,
              })
            },
            onExit: () => {
              mentionMenuOpenRef.current = false
              setSuggestion(null)
            },
            onKeyDown: ({ event }) => mentionMenuRef.current?.onKeyDown(event) ??
              (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Enter'),
          }),
        },
      }),
    ],
    editorProps: {
      attributes: {
        'aria-label': '节点指令',
        'aria-multiline': 'true',
        'data-slot': 'input-group-control',
        role: 'textbox',
        class: 'min-h-28 max-h-48 w-full overflow-y-auto whitespace-pre-wrap bg-transparent px-space-md pt-space-xxs pb-space-sm text-body-md leading-6 text-ink outline-none focus-visible:outline-none',
      },
      handleDOMEvents: {
        blur: (view) => {
          exitSuggestion(view)
          return false
        },
      },
      handleKeyDown: (_view, event) => {
        if (mentionMenuOpenRef.current || event.key !== 'Enter' || event.shiftKey || event.metaKey || event.ctrlKey || event.isComposing) return false
        event.preventDefault()
        const form = event.target instanceof HTMLElement ? event.target.closest('form') : null
        const submitButton = form?.querySelector('button[type="submit"]') as HTMLButtonElement | null
        if (submitButton?.disabled) return true
        form?.requestSubmit()
        return true
      },
    },
    onCreate: ({ editor: nextEditor }) => emitChange(nextEditor),
    onUpdate: ({ editor: nextEditor }) => emitChange(nextEditor),
  }, [])

  useEffect(() => {
    editor?.setEditable(!disabled)
  }, [disabled, editor])

  useImperativeHandle(ref, () => ({
    removeContextNode(nodeId) {
      if (editor === null) return
      editor.chain().focus().command(({ dispatch, state, tr }) => {
        const mentions: { position: number; size: number }[] = []
        state.doc.descendants((node, position) => {
          if (node.type.name === 'mention' && node.attrs.id === nodeId) {
            mentions.push({ position, size: node.nodeSize })
          }
        })
        if (mentions.length === 0) return false
        for (const mention of mentions.reverse()) {
          tr.delete(mention.position, mention.position + mention.size)
        }
        if (dispatch !== undefined) dispatch(tr)
        return true
      }).run()
    },
  }), [editor])

  const mentionModel = suggestion === null
    ? null
    : getContextNodeMentionMenuModel(availableContextNodes, suggestion.query)

  return (
    <div className="min-w-0 flex-1 self-stretch">
      <textarea aria-hidden="true" className="sr-only" name="message" readOnly tabIndex={-1} value={promptValueRef.current.displayText} />
      <div className="relative">
        {isEmpty ? <span aria-hidden="true" className="pointer-events-none absolute top-space-xxs left-space-md text-body-md leading-6 text-faint">{placeholder}</span> : null}
        <EditorContent
          editor={editor}
          className="min-w-0 [&_.ProseMirror[contenteditable=false]]:cursor-not-allowed [&_.ProseMirror[contenteditable=false]]:text-faint [&_.ProseMirror>p]:m-0 [&_.ProseMirror>p+p]:mt-space-xs [&_.canvas-prompt-mention]:mx-0.5 [&_.canvas-prompt-mention]:inline-flex [&_.canvas-prompt-mention]:cursor-default [&_.canvas-prompt-mention]:items-center [&_.canvas-prompt-mention]:rounded-sm [&_.canvas-prompt-mention]:border [&_.canvas-prompt-mention]:border-hairline [&_.canvas-prompt-mention]:bg-hairline-soft [&_.canvas-prompt-mention]:px-1.5 [&_.canvas-prompt-mention]:py-0.5 [&_.canvas-prompt-mention]:text-body-sm [&_.canvas-prompt-mention]:font-medium [&_.canvas-prompt-mention]:text-ink [&_.ProseMirror-selectednode.canvas-prompt-mention]:ring-1 [&_.ProseMirror-selectednode.canvas-prompt-mention]:ring-link"
        />
        {mentionModel !== null && suggestion !== null ? (
          <CanvasPromptMentionMenu
            ref={mentionMenuRef}
            disabled={disabled}
            model={mentionModel}
            onSelect={suggestion.command}
          />
        ) : null}
      </div>
    </div>
  )
})

const CanvasPromptMentionMenu = forwardRef<CanvasPromptMentionMenuHandle, CanvasPromptMentionMenuProps>(function CanvasPromptMentionMenu({
  disabled,
  model,
  onSelect,
}, ref) {
  const [selectedOptionId, setSelectedOptionId] = useState('')
  const optionIds = model.options.map((option) => option.id).join('|')
  const selectedOption = model.options.find((option) => option.id === selectedOptionId) ?? model.options[0]

  useEffect(() => {
    setSelectedOptionId(model.options[0]?.id ?? '')
  }, [optionIds])

  useImperativeHandle(ref, () => ({
    onKeyDown(event) {
      if (event.isComposing) return false
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault()
        if (model.options.length === 0) return true
        setSelectedOptionId((current) => {
          const currentIndex = model.options.findIndex((option) => option.id === current)
          const offset = event.key === 'ArrowDown' ? 1 : -1
          const nextIndex = (Math.max(currentIndex, 0) + offset + model.options.length) % model.options.length
          return model.options[nextIndex]?.id ?? ''
        })
        return true
      }
      if (event.key !== 'Enter') return false
      event.preventDefault()
      if (selectedOption !== undefined && !disabled) onSelect(selectedOption)
      return true
    },
  }))

  return (
    <CanvasContextNodeMentionMenu
      disabled={disabled}
      model={model}
      selectedOptionId={selectedOption?.id ?? ''}
      onSelectedOptionChange={setSelectedOptionId}
      onSelect={onSelect}
    />
  )
})

function getCanvasPromptValue(editor: Editor): CanvasPromptValue {
  const contextNodeIds: string[] = []
  const seenContextNodeIds = new Set<string>()
  let displayText = ''
  let requestText = ''

  editor.state.doc.forEach((block, blockIndex) => {
    if (blockIndex > 0) {
      displayText += '\n'
      requestText += '\n'
    }
    block.forEach((node) => {
      const text = serializePromptNode(node)
      displayText += text.display
      requestText += text.request
      if (text.nodeId !== null && !seenContextNodeIds.has(text.nodeId)) {
        seenContextNodeIds.add(text.nodeId)
        contextNodeIds.push(text.nodeId)
      }
    })
  })

  return { contextNodeIds, displayText, document: editor.getJSON(), requestText }
}

function serializePromptNode(node: ProseMirrorNode) {
  if (node.type.name === 'mention') {
    const nodeId = typeof node.attrs.id === 'string' ? node.attrs.id : null
    const label = typeof node.attrs.label === 'string' ? node.attrs.label : nodeId ?? '节点'
    const display = `@${label}`
    return {
      display,
      nodeId,
      // The structured context payload carries type and priority. Do not leak the display title to the Agent.
      request: nodeId === null ? display : `@[canvas-node:${nodeId}]`,
    }
  }
  if (node.type.name === 'hardBreak') return { display: '\n', nodeId: null, request: '\n' }
  return { display: node.text ?? node.textContent, nodeId: null, request: node.text ?? node.textContent }
}
