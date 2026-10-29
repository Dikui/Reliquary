import { computed, inject, useAttrs, type ComputedRef, type InjectionKey, type HTMLAttributes } from 'vue'

export interface FieldContext {
  id: string
  describedBy?: string
  invalid: boolean
}

export const fieldKey: InjectionKey<ComputedRef<FieldContext>> = Symbol('ui-field')

export function useFieldAttrs() {
  const field = inject(fieldKey, undefined)
  const attrs = useAttrs() as HTMLAttributes
  return computed(() => ({
    ...attrs,
    id: attrs.id ?? field?.value.id,
    'aria-invalid': attrs['aria-invalid'] ?? (field?.value.invalid || undefined),
    'aria-describedby': [field?.value.describedBy, attrs['aria-describedby']].filter(Boolean).join(' ') || undefined,
  }))
}
