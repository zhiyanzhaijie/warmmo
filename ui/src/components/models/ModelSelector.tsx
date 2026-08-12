import { useEffect, useMemo } from 'react'

import { useAvailableModels } from '@/apis/model-apis'
import { DeepSeekLogo, OpenAILogo } from '@/components/svgs/model_provider'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import type { EnabledModel, ModelCapability, ModelReference } from '../../types/provider'

const providerLogos: Record<string, React.ComponentType<React.ComponentProps<'svg'>>> = {
  deepseek: DeepSeekLogo,
  openai: OpenAILogo,
}

function ProviderLogo({ providerId, size = 14 }: { providerId: string; size?: number }) {
  const Logo = providerLogos[providerId]
  if (Logo !== undefined) {
    return <Logo className="shrink-0 text-mute" style={{ width: size, height: size }} />
  }
  return (
    <span
      aria-hidden="true"
      className="grid shrink-0 select-none place-items-center rounded-[3px] bg-hairline-soft font-mono uppercase text-mute"
      style={{ width: size, height: size, fontSize: Math.round(size * 0.62) }}
    >
      {providerId.charAt(0)}
    </span>
  )
}

interface ModelSelectorProps {
  capability: ModelCapability
  value: ModelReference | null
  onValueChange: (model: EnabledModel | null) => void
  autoSelectFirst?: boolean
  disabled?: boolean
  className?: string
  ariaLabel?: string
  unavailableLabel?: string
  compact?: boolean
}

export function ModelSelector({
  capability,
  value,
  onValueChange,
  autoSelectFirst = false,
  disabled = false,
  className,
  ariaLabel,
  unavailableLabel,
  compact = false,
}: ModelSelectorProps) {
  const { models, isPending, isError } = useAvailableModels(capability)
  const modelsByValue = useMemo(
    () => new Map(models.map((model) => [toModelValue(model), model])),
    [models],
  )
  const selectedValue = value === null ? undefined : toModelValue(value)
  const selectedModel = selectedValue === undefined ? undefined : modelsByValue.get(selectedValue)
  const selectionAvailable = selectedValue === undefined || modelsByValue.has(selectedValue)

  useEffect(() => {
    if (!autoSelectFirst) return
    if (isError || (!isPending && models.length === 0)) {
      if (value !== null) onValueChange(null)
      return
    }
    if ((value === null || !selectionAvailable) && models[0] !== undefined) {
      onValueChange(models[0])
    }
  }, [autoSelectFirst, isError, isPending, models, onValueChange, selectionAvailable, value])

  const placeholder = isPending
    ? '正在读取模型'
    : isError
      ? '模型服务不可用'
      : models.length === 0
        ? `未配置${capability === 'text' ? '文本' : capability === 'image' ? '图像' : '嵌入'}模型`
        : '选择模型'

  return (
    <Select
      value={selectedValue}
      onValueChange={(nextValue) => {
        const model = modelsByValue.get(nextValue)
        if (model !== undefined) onValueChange(model)
      }}
      disabled={disabled || isPending || isError || models.length === 0}
    >
      <SelectTrigger className={cn('w-auto min-w-44 border-transparent bg-transparent hover:bg-hairline-soft', className)} aria-label={ariaLabel ?? `选择${capability === 'text' ? '文本' : capability === 'image' ? '图像' : '嵌入'}模型`}>
        <SelectValue placeholder={placeholder}>
          {compact && selectedValue !== undefined ? (
            <span className="flex min-w-0 items-center gap-space-xs">
              <ProviderLogo providerId={selectedModel?.providerId ?? value?.providerId ?? ''} size={13} />
              <span className="truncate">{selectedModel?.modelName ?? value?.modelId}</span>
            </span>
          ) : undefined}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        {!selectionAvailable && selectedValue !== undefined ? (
          <SelectItem value={selectedValue} disabled>{unavailableLabel ?? value?.modelId}（不可用）</SelectItem>
        ) : null}
        {models.map((model) => (
          <SelectItem key={toModelValue(model)} value={toModelValue(model)}>
            <span className="flex min-w-0 items-center gap-space-xs">
              <ProviderLogo providerId={model.providerId} size={14} />
              <span className="truncate">{model.modelName}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function toModelValue(model: ModelReference) {
  return `${model.providerId}:${model.modelId}`
}
