export const KB_EDITOR_FOCUS_SECTION_EVENT = 'reliquary:kb-editor-focus-section'
export const AGENT_EDITOR_FOCUS_SECTION_EVENT = 'reliquary:agent-editor-focus-section'

export function focusKbEditorSection(section: string) {
  window.dispatchEvent(new CustomEvent(KB_EDITOR_FOCUS_SECTION_EVENT, { detail: { section } }))
}

export function focusAgentEditorSection(section: string) {
  window.dispatchEvent(new CustomEvent(AGENT_EDITOR_FOCUS_SECTION_EVENT, { detail: { section } }))
}
