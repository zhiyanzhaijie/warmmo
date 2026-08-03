import { useEffect, useMemo } from 'react'

import { useAvailableModels } from '@/apis/model-apis'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import type { EnabledModel, ModelCapability, ModelReference } from '../../types/provider'

interface ModelSelectorProps {
  capability: ModelCapability
  value: ModelReference | null
  onValueChange: (model: EnabledModel | null) => void
  autoSelectFirst?: boolean
  disabled?: boolean
  className?: string
  ariaLabel?: string
  unavailableLabel?: string
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
}: ModelSelectorProps) {
  const { models, isPending, isError } = useAvailableModels(capability)
  const modelsByValue = useMemo(
    () => new Map(models.map((model) => [toModelValue(model), model])),
    [models],
  )
  const selectedValue = value === null ? undefined : toModelValue(value)
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
        ? `未配置${capability === 'text' ? '文本' : '图像'}模型`
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
      <SelectTrigger className={cn('w-auto min-w-44 border-transparent bg-transparent hover:bg-hairline-soft', className)} aria-label={ariaLabel ?? `选择${capability === 'text' ? '文本' : '图像'}模型`}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {!selectionAvailable && selectedValue !== undefined ? (
          <SelectItem value={selectedValue} disabled>{unavailableLabel ?? value?.modelId}（不可用）</SelectItem>
        ) : null}
        {models.map((model) => (
          <SelectItem key={toModelValue(model)} value={toModelValue(model)}>
            {model.providerName} · {model.modelName}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function toModelValue(model: ModelReference) {
  return `${model.providerId}:${model.modelId}`
}
