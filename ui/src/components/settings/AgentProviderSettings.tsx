import {
  CircleCheck,
  CircleX,
  Eye,
  EyeOff,
  KeyRound,
  LoaderCircle,
  Pencil,
  PlugZap,
  Plus,
  Trash2,
  X,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import type { ReactNode } from 'react'

import {
  useDeleteProvider,
  useModelCatalog,
  useProviderConfigurations,
  useSaveProvider,
  useTestProvider,
} from '@/apis/provider-apis'
import { Checkbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

import type {
  ModelCapability,
  ProviderConfiguration,
  ProviderDefinition,
  ProviderTestResult,
  SaveProviderConfiguration,
} from '../../types/provider'

interface EditorState {
  capability: ModelCapability
  providerId: string
  baseUrl: string
  modelIds: string[]
  apiKey: string
  editing: boolean
}

const capabilityLabels: Record<ModelCapability, string> = {
  text: 'Text Model',
  image: 'Image Model',
}

export function AgentProviderSettings() {
  const {
    data: catalogData,
    error: catalogError,
    isError: isCatalogError,
    isPending: isCatalogPending,
  } = useModelCatalog()
  const {
    data: configurationsData,
    error: configurationsError,
    isError: isConfigurationsError,
    isPending: isConfigurationsPending,
  } = useProviderConfigurations()
  const { mutateAsync: saveProvider, isPending: isSaving } = useSaveProvider()
  const { mutateAsync: deleteProvider } = useDeleteProvider()
  const [editor, setEditor] = useState<EditorState | null>(null)
  const [showAPIKey, setShowAPIKey] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  if (isCatalogPending || isConfigurationsPending) {
    return <StatusPanel icon={<LoaderCircle className="animate-spin" size={18} />} message="正在读取本地模型配置" />
  }
  if (isCatalogError || isConfigurationsError) {
    const error = catalogError ?? configurationsError
    return <StatusPanel icon={<X size={18} />} message={error instanceof Error ? error.message : '无法连接本地 Core'} tone="error" />
  }

  const providers = catalogData.providers
  const configurations = configurationsData.configurations

  const openNewEditor = () => {
    const provider = providers.find((candidate) =>
      candidate.models.some((model) => model.capability === 'text')
      && !configurations.some((configuration) => configuration.providerId === candidate.id),
    ) ?? providers.find((candidate) => candidate.models.some((model) => model.capability === 'text'))
    if (provider === undefined) return

    const existing = configurations.find((configuration) => configuration.providerId === provider.id)
    setEditor({
      capability: 'text',
      providerId: provider.id,
      baseUrl: existing?.baseUrl ?? provider.defaultBaseUrl,
      modelIds: existing?.modelIds ?? [],
      apiKey: '',
      editing: existing !== undefined,
    })
    setFormError(null)
  }

  const openEditEditor = (configuration: ProviderConfiguration) => {
    const provider = providers.find((candidate) => candidate.id === configuration.providerId)
    if (provider === undefined) return
    const selectedModel = provider.models.find((model) => configuration.modelIds.includes(model.id))
    setEditor({
      capability: selectedModel?.capability ?? 'text',
      providerId: provider.id,
      baseUrl: configuration.baseUrl,
      modelIds: configuration.modelIds,
      apiKey: '',
      editing: true,
    })
    setFormError(null)
  }

  const handleSave = async (input: SaveProviderConfiguration) => {
    setFormError(null)
    try {
      await saveProvider(input)
      setEditor(null)
      setShowAPIKey(false)
    } catch (error) {
      setFormError(error instanceof Error ? error.message : '保存失败')
    }
  }

  const handleDelete = async (providerId: string) => {
    setFormError(null)
    try {
      await deleteProvider(providerId)
      if (editor?.providerId === providerId) setEditor(null)
    } catch (error) {
      setFormError(error instanceof Error ? error.message : '删除失败')
    }
  }

  return (
    <section aria-labelledby="agent-provider-heading">
      <div className="flex items-start justify-between gap-space-xl border-b border-hairline pb-space-lg">
        <div>
          <h2 id="agent-provider-heading" className="text-heading-md">Agent Provider</h2>
          <p className="mt-space-xs text-body-md text-body">配置后，所选模型才会出现在生成菜单中。</p>
        </div>
        <button
          type="button"
          onClick={openNewEditor}
          className="flex h-9 shrink-0 items-center gap-space-xs rounded-sm bg-primary px-space-sm text-button-md text-on-primary transition-opacity hover:opacity-85"
        >
          <Plus size={15} /> 添加 Provider
        </button>
      </div>

      {formError !== null && editor === null ? (
        <p className="mt-space-md rounded-sm border border-error/25 bg-error/5 px-space-sm py-space-xs text-body-sm text-error">{formError}</p>
      ) : null}

      {editor !== null ? (
        <ProviderEditor
          editor={editor}
          providers={providers}
          configurations={configurations}
          error={formError}
          showAPIKey={showAPIKey}
          submitting={isSaving}
          onChange={setEditor}
          onShowAPIKeyChange={setShowAPIKey}
          onCancel={() => { setEditor(null); setFormError(null); setShowAPIKey(false) }}
          onSave={handleSave}
        />
      ) : null}

      <div className="mt-space-xl">
        <div className="flex items-baseline justify-between">
          <h3 className="text-label-sm">已配置</h3>
          <span className="font-mono text-body-sm text-mute">{configurations.length} PROVIDERS</span>
        </div>
        {configurations.length === 0 ? (
          <div className="mt-space-sm grid min-h-40 place-items-center rounded-md border border-dashed border-hairline text-center">
            <div>
              <KeyRound className="mx-auto text-mute" size={20} />
              <p className="mt-space-sm text-label-sm">还没有可用模型</p>
              <p className="mt-space-xxs text-body-sm text-mute">添加一个 Provider 开始使用。</p>
            </div>
          </div>
        ) : (
          <div className="mt-space-sm divide-y divide-hairline rounded-md border border-hairline bg-canvas-elevated">
            {configurations.map((configuration) => (
              <ProviderRow
                key={configuration.id}
                configuration={configuration}
                provider={providers.find((candidate) => candidate.id === configuration.providerId)}
                onEdit={() => openEditEditor(configuration)}
                onDelete={() => {
                  const providerName = providers.find((candidate) => candidate.id === configuration.providerId)?.name ?? configuration.providerId
                  if (window.confirm(`删除 ${providerName} 配置？相关模型将从所有模型菜单中移除。`)) {
                    void handleDelete(configuration.providerId)
                  }
                }}
              />
            ))}
          </div>
        )}
      </div>
    </section>
  )
}

function ProviderEditor({
  editor,
  providers,
  configurations,
  error,
  showAPIKey,
  submitting,
  onChange,
  onShowAPIKeyChange,
  onCancel,
  onSave,
}: {
  editor: EditorState
  providers: ProviderDefinition[]
  configurations: ProviderConfiguration[]
  error: string | null
  showAPIKey: boolean
  submitting: boolean
  onChange: (editor: EditorState) => void
  onShowAPIKeyChange: (show: boolean) => void
  onCancel: () => void
  onSave: (input: SaveProviderConfiguration) => Promise<void>
}) {
  const {
    data: testResult,
    error: testError,
    isPending: isTesting,
    mutate: testProvider,
    reset: resetTest,
  } = useTestProvider()
  const availableProviders = useMemo(
    () => providers.filter((provider) => provider.models.some((model) => model.capability === editor.capability)),
    [editor.capability, providers],
  )
  const provider = providers.find((candidate) => candidate.id === editor.providerId) ?? availableProviders[0]
  const availableModels = provider?.models.filter((model) => model.capability === editor.capability) ?? []
  const existingConfiguration = configurations.find((configuration) => configuration.providerId === editor.providerId)
  const hasCurrentModel = availableModels.some((model) => editor.modelIds.includes(model.id))
  const needsAPIKey = existingConfiguration === undefined
  const canSave = hasCurrentModel && editor.baseUrl.trim() !== '' && (!needsAPIKey || editor.apiKey.trim() !== '')
  const canTest = editor.baseUrl.trim() !== '' && (editor.apiKey.trim() !== '' || existingConfiguration !== undefined)

  const updateEditor = (next: EditorState) => {
    resetTest()
    onChange(next)
  }

  const changeCapability = (capability: ModelCapability) => {
    const currentProvider = providers.find((candidate) => candidate.id === editor.providerId)
    const nextProvider = currentProvider?.models.some((model) => model.capability === capability)
      ? currentProvider
      : providers.find((candidate) => candidate.models.some((model) => model.capability === capability))
    if (nextProvider === undefined) return
    const existing = configurations.find((configuration) => configuration.providerId === nextProvider.id)
    updateEditor({
      ...editor,
      capability,
      providerId: nextProvider.id,
      baseUrl: existing?.baseUrl ?? nextProvider.defaultBaseUrl,
      modelIds: existing?.modelIds ?? (nextProvider.id === editor.providerId ? editor.modelIds : []),
      apiKey: '',
      editing: existing !== undefined,
    })
  }

  const changeProvider = (providerId: string) => {
    const nextProvider = providers.find((candidate) => candidate.id === providerId)
    if (nextProvider === undefined) return
    const existing = configurations.find((configuration) => configuration.providerId === providerId)
    updateEditor({
      ...editor,
      providerId,
      baseUrl: existing?.baseUrl ?? nextProvider.defaultBaseUrl,
      modelIds: existing?.modelIds ?? [],
      apiKey: '',
      editing: existing !== undefined,
    })
  }

  const toggleModel = (modelId: string) => {
    updateEditor({
      ...editor,
      modelIds: editor.modelIds.includes(modelId)
        ? editor.modelIds.filter((candidate) => candidate !== modelId)
        : [...editor.modelIds, modelId],
    })
  }

  const handleTest = () => {
    if (provider === undefined || !canTest) return
    testProvider({
      providerId: provider.id,
      input: { baseUrl: editor.baseUrl, apiKey: editor.apiKey },
    })
  }

  return (
    <form
      className="mt-space-lg rounded-md border border-hairline bg-canvas-elevated p-space-lg shadow-whisper"
      onSubmit={(event) => {
        event.preventDefault()
        if (provider !== undefined && canSave) {
          void onSave({ providerId: provider.id, baseUrl: editor.baseUrl, modelIds: editor.modelIds, apiKey: editor.apiKey })
        }
      }}
    >
      <div className="flex items-center justify-between">
        <h3 className="text-label-sm">{editor.editing ? `编辑 ${provider?.name ?? ''}` : '添加 Provider'}</h3>
        <button type="button" onClick={onCancel} className="grid size-8 place-items-center rounded-sm text-mute hover:bg-hairline-soft hover:text-ink" aria-label="关闭配置表单">
          <X size={16} />
        </button>
      </div>

      <div className="mt-space-md grid grid-cols-2 gap-space-md">
        <Field label="模型类型">
          <Select value={editor.capability} onValueChange={(value) => changeCapability(value as ModelCapability)}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="text">Text Model</SelectItem>
              <SelectItem value="image">Image Model</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        <Field label="Provider">
          <Select value={provider?.id ?? ''} onValueChange={changeProvider}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {availableProviders.map((candidate) => <SelectItem key={candidate.id} value={candidate.id}>{candidate.name}</SelectItem>)}
            </SelectContent>
          </Select>
        </Field>
      </div>

      <fieldset className="mt-space-md">
        <legend className="text-body-sm text-body">模型</legend>
        <div className="mt-space-xs divide-y divide-hairline rounded-sm border border-hairline">
          {availableModels.map((model) => {
            const checked = editor.modelIds.includes(model.id)
            return (
              <label key={model.id} htmlFor={`provider-model-${model.id}`} className="flex cursor-pointer items-center gap-space-sm px-space-sm py-space-xs hover:bg-hairline-soft">
                <Checkbox id={`provider-model-${model.id}`} checked={checked} onCheckedChange={() => toggleModel(model.id)} />
                <span className="min-w-0 flex-1">
                  <span className="text-label-sm">{model.name}</span>
                  <span className="ml-space-xs text-body-sm text-mute">{model.description}</span>
                </span>
              </label>
            )
          })}
        </div>
      </fieldset>

      <div className="mt-space-md">
        <label className="block text-body-sm text-body" htmlFor="provider-api-key">API Key</label>
        <div className="mt-space-xs flex max-w-2xl gap-space-xs">
          <span className="relative block min-w-0 flex-1">
            <input
              id="provider-api-key"
              value={editor.apiKey}
              onChange={(event) => updateEditor({ ...editor, apiKey: event.target.value })}
              type={showAPIKey ? 'text' : 'password'}
              placeholder={existingConfiguration !== undefined ? '留空以使用已保存密钥' : '输入 API Key'}
              className="h-10 w-full rounded-sm border border-hairline bg-canvas px-space-sm pr-11 font-mono text-code text-ink outline-none placeholder:font-sans placeholder:text-mute focus:border-link focus:ring-2 focus:ring-link-soft"
              autoComplete="new-password"
              spellCheck={false}
            />
            <button
              type="button"
              onClick={() => onShowAPIKeyChange(!showAPIKey)}
              className="absolute right-1 top-1 grid size-8 place-items-center rounded-sm text-mute hover:bg-hairline-soft hover:text-ink"
              aria-label={showAPIKey ? '隐藏密钥' : '显示密钥'}
            >
              {showAPIKey ? <EyeOff size={15} /> : <Eye size={15} />}
            </button>
          </span>
          <button
            type="button"
            onClick={handleTest}
            disabled={!canTest || isTesting}
            className="flex h-10 min-w-24 items-center justify-center gap-space-xs rounded-sm border border-hairline px-space-sm text-button-md hover:bg-hairline-soft disabled:cursor-not-allowed disabled:opacity-40"
          >
            {isTesting ? <LoaderCircle className="animate-spin" size={14} /> : <PlugZap size={14} />}
            测试连接
          </button>
        </div>
        <p className="mt-space-xs text-body-sm text-mute">密钥由本机 Core 加密保存，前端不会读取已保存的原文。</p>
        <TestFeedback result={testResult ?? null} error={testError} />
      </div>

      <details className="mt-space-md group">
        <summary className="w-fit cursor-pointer text-body-sm text-body hover:text-ink">高级设置</summary>
        <label className="mt-space-xs block max-w-2xl text-body-sm text-body">
          API Base URL
          <input
            value={editor.baseUrl}
            onChange={(event) => updateEditor({ ...editor, baseUrl: event.target.value })}
            className="mt-space-xs h-10 w-full rounded-sm border border-hairline bg-canvas px-space-sm text-body-md text-ink outline-none focus:border-link focus:ring-2 focus:ring-link-soft"
            spellCheck={false}
            autoComplete="off"
          />
        </label>
      </details>

      {error !== null ? <p className="mt-space-md text-body-sm text-error">{error}</p> : null}

      <div className="mt-space-md flex justify-end gap-space-xs border-t border-hairline pt-space-md">
        <button type="button" onClick={onCancel} className="h-9 rounded-sm border border-hairline px-space-sm text-button-md hover:bg-hairline-soft">取消</button>
        <button type="submit" disabled={!canSave || submitting} className="flex h-9 min-w-20 items-center justify-center gap-space-xs rounded-sm bg-primary px-space-sm text-button-md text-on-primary disabled:cursor-not-allowed disabled:opacity-40">
          {submitting ? <LoaderCircle className="animate-spin" size={14} /> : null}
          保存
        </button>
      </div>
    </form>
  )
}

function TestFeedback({ result, error }: { result: ProviderTestResult | null; error: Error | null }) {
  if (error !== null) {
    return <p className="mt-space-xs flex items-center gap-space-xs text-body-sm text-error"><CircleX size={14} />{error.message}</p>
  }
  if (result === null) return null
  return (
    <p className={`mt-space-xs flex items-center gap-space-xs text-body-sm ${result.valid ? 'text-link' : 'text-error'}`}>
      {result.valid ? <CircleCheck size={14} /> : <CircleX size={14} />}
      {result.message} · {result.latencyMs} ms
    </p>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div className="text-body-sm text-body"><p>{label}</p><div className="mt-space-xs">{children}</div></div>
}

function ProviderRow({ configuration, provider, onEdit, onDelete }: {
  configuration: ProviderConfiguration
  provider?: ProviderDefinition
  onEdit: () => void
  onDelete: () => void
}) {
  const models = provider?.models.filter((model) => configuration.modelIds.includes(model.id)) ?? []
  return (
    <div className="flex min-h-20 items-center gap-space-md px-space-lg py-space-sm">
      <span className="grid size-9 shrink-0 place-items-center rounded-sm bg-hairline-soft font-mono text-body-sm">{provider?.name.slice(0, 2).toUpperCase() ?? '?'}</span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-space-sm">
          <h4 className="text-label-sm">{provider?.name ?? configuration.providerId}</h4>
          <span className="font-mono text-body-sm text-mute">{configuration.apiKeyHint}</span>
        </div>
        <p className="mt-space-xxs truncate text-body-sm text-mute">{models.map((model) => model.name).join(' · ')}</p>
      </div>
      <div className="flex shrink-0 items-center gap-space-xxs">
        <button type="button" onClick={onEdit} className="grid size-8 place-items-center rounded-sm text-mute hover:bg-hairline-soft hover:text-ink" aria-label={`编辑 ${provider?.name ?? configuration.providerId}`}><Pencil size={15} /></button>
        <button type="button" onClick={onDelete} className="grid size-8 place-items-center rounded-sm text-mute hover:bg-error/10 hover:text-error" aria-label={`删除 ${provider?.name ?? configuration.providerId}`}><Trash2 size={15} /></button>
      </div>
    </div>
  )
}

function StatusPanel({ icon, message, tone = 'normal' }: { icon: ReactNode; message: string; tone?: 'normal' | 'error' }) {
  return (
    <div className={`flex min-h-48 items-center justify-center gap-space-sm rounded-md border border-hairline bg-canvas-elevated text-body-md ${tone === 'error' ? 'text-error' : 'text-body'}`}>
      {icon}{message}
    </div>
  )
}
