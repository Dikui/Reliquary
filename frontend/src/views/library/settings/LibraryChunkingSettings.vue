<template>
  <div class="library-chunking-settings">
    <div class="settings-group">
      <!-- Strategy -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('libraryEditor.chunking.strategyLabel') }}</label>
          <p class="desc">{{ $t('libraryEditor.chunking.strategyDescription') }}</p>
        </div>
        <div class="setting-control strategy-control">
          <select
            v-model="localStrategy" :aria-label="$t('libraryEditor.chunking.strategyLabel')"
            :placeholder="$t('libraryEditor.chunking.strategyPlaceholder')"
            @change="handleStrategyChange"
           class="rq-control rq-select"><option value="">{{ $t('libraryEditor.chunking.strategyPlaceholder') }}</option><option v-for="option in strategyOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select>
        </div>
      </div>

      <!-- Strategy explanation panel -->
      <div v-if="currentStrategyInfo" class="strategy-info-panel">
        <p>
          <strong>{{ currentStrategyInfo.label }}:</strong>
          {{ currentStrategyInfo.tooltip }}
        </p>
      </div>

      <!-- Chunk Size -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('libraryEditor.chunking.sizeLabel') }}</label>
          <p class="desc">{{ $t('libraryEditor.chunking.sizeDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-container">
            <input type="range" class="rq-range"
              v-model.number="localChunkSize" :aria-label="$t('libraryEditor.chunking.sizeLabel')"
              :min="100"
              :max="4000"
              :step="50"
              @change="handleChunkSizeChange"
            />
            <span class="value-display">{{ localChunkSize }} {{ $t('libraryEditor.chunking.characters', localChunkSize) }}</span>
          </div>
        </div>
      </div>

      <!-- Chunk Overlap -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('libraryEditor.chunking.overlapLabel') }}</label>
          <p class="desc">{{ $t('libraryEditor.chunking.overlapDescription') }}</p>
          <p v-if="overlapTooHigh" class="warn">{{ $t('libraryEditor.chunking.overlapWarning') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-container">
            <input type="range" class="rq-range"
              v-model.number="localChunkOverlap" :aria-label="$t('libraryEditor.chunking.overlapLabel')"
              :min="0"
              :max="500"
              :step="20"
              @change="handleChunkOverlapChange"
            />
            <span class="value-display">{{ localChunkOverlap }} {{ $t('libraryEditor.chunking.characters', localChunkOverlap) }}</span>
          </div>
        </div>
      </div>

      <!-- Separators -->
      <div class="setting-row setting-row--separators">
        <div class="setting-info">
          <label>{{ $t('libraryEditor.chunking.separatorsLabel') }}</label>
          <p class="desc">{{ $t('libraryEditor.chunking.separatorsDescription') }}</p>
        </div>
        <div class="setting-control">
          <select
            v-model="localSeparators" :aria-label="$t('libraryEditor.chunking.separatorsLabel')"
            multiple
            :placeholder="$t('libraryEditor.chunking.separatorsPlaceholder')"
            @change="handleSeparatorsChange"
           class="rq-control rq-select"><option v-for="option in availableSeparators" :key="option.value" :value="option.value">{{ option.label }}</option></select><div class="separator-create"><Input v-model="separatorDraft" :aria-label="$t('libraryEditor.chunking.separatorsPlaceholder')" :placeholder="$t('libraryEditor.chunking.separatorsPlaceholder')" @keydown.enter.prevent="addSeparator" /><Button :disabled="!separatorDraft" @click="addSeparator">{{ $t('common.add') }}</Button></div>
        </div>
      </div>

      <!-- Parent-Child Chunking -->
      <div class="setting-row setting-row--toggle">
        <div class="setting-info">
          <label>{{ $t('libraryEditor.chunking.parentChildLabel') }}</label>
          <p class="desc">{{ $t('libraryEditor.chunking.parentChildDescription') }}</p>
        </div>
        <div class="setting-control">
          <Switch
            v-model="localEnableParentChild" :aria-label="$t('libraryEditor.chunking.parentChildLabel')"
            @update:model-value="handleParentChildChange"
          />
        </div>
      </div>

      <!-- Parent Chunk Size -->
      <div v-if="localEnableParentChild" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('libraryEditor.chunking.parentChunkSizeLabel') }}</label>
          <p class="desc">{{ $t('libraryEditor.chunking.parentChunkSizeDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-container">
            <input type="range" class="rq-range"
              v-model.number="localParentChunkSize" :aria-label="$t('libraryEditor.chunking.parentChunkSizeLabel')"
              :min="512"
              :max="8192"
              :step="64"
              @change="handleParentChunkSizeChange"
            />
            <span class="value-display">{{ localParentChunkSize }} {{ $t('libraryEditor.chunking.characters', localParentChunkSize) }}</span>
          </div>
        </div>
      </div>

      <!-- Child Chunk Size -->
      <div v-if="localEnableParentChild" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('libraryEditor.chunking.childChunkSizeLabel') }}</label>
          <p class="desc">{{ $t('libraryEditor.chunking.childChunkSizeDescription') }}</p>
        </div>
        <div class="setting-control">
          <div class="slider-container">
            <input type="range" class="rq-range"
              v-model.number="localChildChunkSize" :aria-label="$t('libraryEditor.chunking.childChunkSizeLabel')"
              :min="64"
              :max="2048"
              :step="32"
              @change="handleChildChunkSizeChange"
            />
            <span class="value-display">{{ localChildChunkSize }} {{ $t('libraryEditor.chunking.characters', localChildChunkSize) }}</span>
          </div>
        </div>
      </div>

      <!-- Advanced section toggle -->
      <button type="button" class="advanced-toggle" :aria-expanded="advancedOpen" @click="advancedOpen = !advancedOpen">
        <span aria-hidden="true">{{ advancedOpen ? '⌄' : '›' }}</span>
        <span>{{ $t('libraryEditor.chunking.advancedLabel') }}</span>
      </button>

      <div v-if="advancedOpen" class="advanced-section">
        <!-- Token Limit -->
        <div class="setting-row" :class="{ disabled: advancedDisabled }">
          <div class="setting-info">
            <label>{{ $t('libraryEditor.chunking.tokenLimitLabel') }}</label>
            <p class="desc">{{ $t('libraryEditor.chunking.tokenLimitDescription') }}</p>
          </div>
          <div class="setting-control">
            <input type="number" class="rq-control"
              v-model.number="localTokenLimit" :aria-label="$t('libraryEditor.chunking.tokenLimitLabel')"
              :min="0"
              :max="8192"
              :step="64"
              :disabled="advancedDisabled"
              @change="handleTokenLimitChange"

            />
          </div>
        </div>

        <!-- Languages -->
        <div class="setting-row" :class="{ disabled: advancedDisabled }">
          <div class="setting-info">
            <label>{{ $t('libraryEditor.chunking.languagesLabel') }}</label>
            <p class="desc">{{ $t('libraryEditor.chunking.languagesDescription') }}</p>
          </div>
          <div class="setting-control">
            <select
              v-model="localLanguages" :aria-label="$t('libraryEditor.chunking.languagesLabel')"
              multiple
              :disabled="advancedDisabled"
              :placeholder="$t('libraryEditor.chunking.languagesPlaceholder')"
              @change="handleLanguagesChange"
             class="rq-control rq-select"><option v-for="option in languageOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select>
          </div>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Input, Switch } from '@/components/ui'

// Native range bounds mirror
// the validated bounds in the backend splitter:
//   ChunkSize:      100–4000  (default 512). 100 = too fragmented to be
//                   useful; 4000 = approaches the 7500-char absoluteMaxSize
//                   that the splitter hard-caps to anyway.
//   ChunkOverlap:   0–500     (default 80). Backend caps to ChunkSize/2
//                   when set higher than that.
//   ParentChunkSize: 512–8192 (default 4096 ≈ 1000 EN tokens).
//   ChildChunkSize:  64–2048  (default 384 ≈ 80 EN tokens, sweet spot for
//                   sentence-transformer / BGE embedders).
//   TokenLimit:      0–8192   (default 0 = off, char-based budget only).
//                   Set to 200 for MiniLM (256-tok limit), 400 for BGE/
//                   Cohere (512-tok), leave at 0 for OpenAI/Voyage/Jina-v3.
interface ChunkingConfig {
  chunkSize: number
  chunkOverlap: number
  separators: string[]
  enableParentChild: boolean
  parentChunkSize: number
  childChunkSize: number
  // Adaptive chunking strategy. Empty string = legacy / not set.
  strategy?: string
  // Cap chunk size in approx tokens. 0 = char-based budget only.
  tokenLimit?: number
  // Language hints for heuristic patterns (de/en/zh).
  languages?: string[]
}

interface Props {
  config: ChunkingConfig
}

const props = defineProps<Props>()

const emit = defineEmits<{
  'update:config': [value: ChunkingConfig]
}>()

const { t } = useI18n()

const localChunkSize = ref(props.config.chunkSize)
const localChunkOverlap = ref(props.config.chunkOverlap)
const localSeparators = ref([...props.config.separators])
const localEnableParentChild = ref(props.config.enableParentChild ?? false)
const localParentChunkSize = ref(props.config.parentChunkSize || 4096)
const localChildChunkSize = ref(props.config.childChunkSize || 384)
const localStrategy = ref(props.config.strategy ?? '')
const localTokenLimit = ref(props.config.tokenLimit ?? 0)
const localLanguages = ref<string[]>([...(props.config.languages ?? [])])
const advancedOpen = ref(false)
const separatorDraft = ref('')
function addSeparator() {
  if (separatorDraft.value && !localSeparators.value.includes(separatorDraft.value)) { localSeparators.value.push(separatorDraft.value); emitUpdate() }
  separatorDraft.value = ''
}

const strategyOptions = computed(() => [
  {
    label: t('libraryEditor.chunking.strategies.auto.label'),
    value: 'auto',
    tooltip: t('libraryEditor.chunking.strategies.auto.tooltip')
  },
  {
    label: t('libraryEditor.chunking.strategies.heading.label'),
    value: 'heading',
    tooltip: t('libraryEditor.chunking.strategies.heading.tooltip')
  },
  {
    label: t('libraryEditor.chunking.strategies.heuristic.label'),
    value: 'heuristic',
    tooltip: t('libraryEditor.chunking.strategies.heuristic.tooltip')
  },
  {
    label: t('libraryEditor.chunking.strategies.legacy.label'),
    value: 'legacy',
    tooltip: t('libraryEditor.chunking.strategies.legacy.tooltip')
  }
])

const currentStrategyInfo = computed(() => {
  if (!localStrategy.value) {
    return null
  }
  return strategyOptions.value.find(o => o.value === localStrategy.value) ?? null
})

const advancedDisabled = computed(() => localStrategy.value === 'legacy')

const overlapTooHigh = computed(
  () => localChunkOverlap.value > 0 && localChunkOverlap.value >= localChunkSize.value / 2
)

const languageOptions = computed(() => [
  { label: t('libraryEditor.chunking.languageOptions.de'), value: 'de' },
  { label: t('libraryEditor.chunking.languageOptions.en'), value: 'en' },
  { label: t('libraryEditor.chunking.languageOptions.zh'), value: 'zh' }
])

const separatorOptions = computed(() => [
  { label: t('libraryEditor.chunking.separators.doubleNewline'), value: '\n\n' },
  { label: t('libraryEditor.chunking.separators.singleNewline'), value: '\n' },
  { label: t('libraryEditor.chunking.separators.periodCn'), value: '。' },
  { label: t('libraryEditor.chunking.separators.exclamationCn'), value: '！' },
  { label: t('libraryEditor.chunking.separators.questionCn'), value: '？' },
  { label: t('libraryEditor.chunking.separators.semicolonCn'), value: '；' },
  { label: t('libraryEditor.chunking.separators.semicolonEn'), value: ';' },
  { label: t('libraryEditor.chunking.separators.space'), value: ' ' }
])

const availableSeparators = computed(() => [...separatorOptions.value, ...localSeparators.value.filter(value => !separatorOptions.value.some(option => option.value === value)).map(value => ({ value, label: value }))])

watch(() => props.config, (newConfig) => {
  localChunkSize.value = newConfig.chunkSize
  localChunkOverlap.value = newConfig.chunkOverlap
  localSeparators.value = [...newConfig.separators]
  localEnableParentChild.value = newConfig.enableParentChild ?? false
  localParentChunkSize.value = newConfig.parentChunkSize || 4096
  localChildChunkSize.value = newConfig.childChunkSize || 384
  localStrategy.value = newConfig.strategy ?? ''
  localTokenLimit.value = newConfig.tokenLimit ?? 0
  localLanguages.value = [...(newConfig.languages ?? [])]
}, { deep: true })

const handleChunkSizeChange = () => { emitUpdate() }
const handleChunkOverlapChange = () => { emitUpdate() }
const handleSeparatorsChange = () => { emitUpdate() }
const handleParentChildChange = () => { emitUpdate() }
const handleParentChunkSizeChange = () => { emitUpdate() }
const handleChildChunkSizeChange = () => { emitUpdate() }
const handleStrategyChange = () => { emitUpdate() }
const handleTokenLimitChange = () => { emitUpdate() }
const handleLanguagesChange = () => { emitUpdate() }

const emitUpdate = () => {
  // Spread arrays so the parent gets its own copy. Mutating the emitted
  // arrays from outside must not leak back into our reactive state and
  // cause two-way ref drift between the form and the editor model.
  emit('update:config', {
    chunkSize: localChunkSize.value,
    chunkOverlap: localChunkOverlap.value,
    separators: [...localSeparators.value],
    enableParentChild: localEnableParentChild.value,
    parentChunkSize: localParentChunkSize.value,
    childChunkSize: localChildChunkSize.value,
    strategy: localStrategy.value,
    tokenLimit: localTokenLimit.value,
    languages: [...localLanguages.value]
  })
}
</script>

<style scoped>
.library-chunking-settings { min-width: 0; width: 100%; color: var(--text); }
.settings-group { display: grid; }
.setting-row { display: grid; gap: 10px; padding: 16px 0; border-top: 2px solid var(--rule); }
.setting-info label { display: block; font-size: var(--text-sm); font-weight: 700; }
.desc, .warn { margin: 4px 0 0; font-size: var(--text-xs); color: var(--muted); }
.warn { color: var(--warning); }
.setting-control, .strategy-control { display: grid; gap: 8px; min-width: 0; }
.strategy-info-panel { margin-bottom: 16px; padding: 12px; border-left: 2px solid var(--rule); background: var(--surface-soft); font-size: var(--text-sm); }
.strategy-info-panel p { margin: 0; }
.slider-container { display: grid; gap: 8px; }
.value-display { order: -1; font: var(--text-sm) var(--mono); }
.rq-range { accent-color: var(--text); width: 100%; min-width: 0; }
.advanced-toggle { display: flex; gap: 8px; padding: 12px 0; border: 0; color: var(--text); background: transparent; font: inherit; text-align: left; cursor: pointer; }
.advanced-toggle:hover { text-decoration: underline; }
.advanced-toggle:focus-visible { outline: 2px solid var(--rule); }
.separator-create { display: flex; flex-wrap: wrap; gap: 8px; }
.separator-create :deep(.rq-control) { flex: 1; min-width: 160px; }
</style>
