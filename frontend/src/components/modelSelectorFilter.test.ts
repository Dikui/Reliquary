import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import type { ModelConfig } from '@/api/model'

import { filterModelsByType } from './modelSelectorFilter.ts'

const selectorSource = readFileSync(new URL('./ModelSelector.vue', import.meta.url), 'utf8')
const libraryEditorSource = readFileSync(new URL('../views/settings/LibrarySettings.vue', import.meta.url), 'utf8')
const modelSettingsSource = readFileSync(new URL('../views/settings/ModelSettings.vue', import.meta.url), 'utf8')

function model(overrides: Partial<ModelConfig> & Pick<ModelConfig, 'id' | 'type'>): ModelConfig {
  return {
    name: overrides.name ?? overrides.id!,
    source: 'remote',
    parameters: {},
    ...overrides,
  }
}

const fixtures: ModelConfig[] = [
  model({ id: 'chat-vision', name: 'MiniMax-M3', display_name: 'MiniMax M3', type: 'KnowledgeQA', parameters: { supports_vision: true } }),
  model({ id: 'chat-text', type: 'KnowledgeQA' }),
  model({ id: 'chat-no-vision', type: 'KnowledgeQA', parameters: { supports_vision: false } }),
  model({ id: 'embed-vision', type: 'Embedding', parameters: { supports_vision: true } }),
  model({ id: 'embed-1', type: 'Embedding' }),
]

test('model selectors filter by type and preserve vision-capable generation models', () => {
  const ids = filterModelsByType(fixtures, 'KnowledgeQA').map((m) => m.id)
  assert.deepEqual(ids, ['chat-vision', 'chat-text', 'chat-no-vision'])
})

test('model labels, values and deletion preserve the model identity', () => {
  assert.match(selectorSource, /:label="modelDisplayName\(model\)"/)
  assert.match(selectorSource, /:key="model\.id"[\s\S]*:value="model\.id"/)
  assert.match(selectorSource, /emit\('update:selectedModelId', value \|\| ''\)/)
  assert.match(modelSettingsSource, /await deleteModelAPI\(modelId\)/)
})
