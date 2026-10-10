/** Mirrors backend internal/models/provider.IsQwenThinkingModel */
export function isQwenThinkingModel(modelName: string): boolean {
  const lower = modelName.trim().toLowerCase()
  return (
    lower.startsWith('qwen3')
    || lower.startsWith('qwen-plus')
    || lower.startsWith('qwen-max')
    || lower.startsWith('qwen-turbo')
  )
}

export type ThinkingControlValue =
  | 'none'
  | 'chat_template_kwargs'
  | 'enable_thinking'
  | 'thinking_type'

const THINKING_CONTROL_VALUES: ThinkingControlValue[] = [
  'none',
  'chat_template_kwargs',
  'enable_thinking',
  'thinking_type',
]

/**
 * Default thinking_control for provider+model.
 * Must stay aligned with chat.resolveProvider(...).Thinking() in provider.go.
 */
export function defaultThinkingControl(
  provider: string,
  modelName = '',
): ThinkingControlValue {
  const p = provider.trim().toLowerCase()
  const model = modelName.trim()

  switch (p) {
    case 'aliyun':
      return isQwenThinkingModel(model) ? 'enable_thinking' : 'none'
    case 'volcengine':
      return 'thinking_type'
    case 'openai':
    case 'anthropic':
    case 'zhipu':
    case 'deepseek':
    case 'gemini':
    case 'siliconflow':
    case 'moonshot':
    case 'openrouter':
    case 'ollama':
      return 'none'
    default:
      // 通用兼容接口和已删除的服务商标识使用相同默认值。
      return 'chat_template_kwargs'
  }
}

/** Resolve stored extra_config or fall back to the provider default. */
export function resolveThinkingControl(
  saved: string | undefined,
  provider: string,
  modelName = '',
): ThinkingControlValue {
  const v = saved?.trim().toLowerCase()
  if (THINKING_CONTROL_VALUES.includes(v as ThinkingControlValue)) {
    return v as ThinkingControlValue
  }
  return defaultThinkingControl(provider, modelName)
}

/** Whether the debug drawer should expose Think on/off for this saved model. */
export function modelSupportsThinking(model: {
  type: string
  source: string
  name: string
  parameters: {
    provider?: string
    extra_config?: { thinking_control?: string }
  }
}): boolean {
  if (model.type !== 'KnowledgeQA' || model.source !== 'remote') return false
  return resolveThinkingControl(
    model.parameters.extra_config?.thinking_control,
    model.parameters.provider || '',
    model.name || '',
  ) !== 'none'
}
