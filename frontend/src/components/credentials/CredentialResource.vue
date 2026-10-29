<!--
  CredentialResource — per-field "configured / unconfigured / editing" card.

  Why this component exists:
    The previous UX (PR #990) showed a password input pre-filled with a redacted
    placeholder plus a red "Remove this credential" checkbox below it. That
    conflated three distinct user intents (preserve / replace / clear) into a
    single form field, and bundled credential changes with unrelated config
    edits in the same submit. Users could accidentally wipe a working key by
    toggling the wrong checkbox.

    This component splits credentials out as an independent resource:
      - Read-only "configured" badge by default, derived from the `meta` prop.
      - "Replace" expands an input + Save/Cancel; commit is an explicit
        PUT to the credential subresource (no main-form submit needed).
      - "Remove" pops a danger-themed confirmation and on confirm calls DELETE
        immediately. No "save form to apply" intermediate state.

    `meta` is the source of truth and comes from the parent resource's main
    GET response (`<resource>.credentials` on every DTO) — there is no
    dedicated GET /credentials endpoint. After a successful save/remove the
    component derives the new local state from the save's return value (or,
    for remove, by setting the field to unconfigured) and emits 'changed' so
    the parent can re-fetch the main resource if it cares about anything
    else that depends on credential state.
-->
<template>
  <div class="credential-resource settings-form">
    <div v-for="field in fields" :key="field.key" class="credential-row">
      <!--
        Per-field label, rendered only when there is more than one credential
        (e.g. a signed provider's api_key + app_secret) so a single-field card
        doesn't double up with the parent's outer .form-label. Single-field
        consumers (ModelEditorDialog API key and provider api_key) keep showing the parent label only.
      -->
      <div v-if="fields.length > 1" class="credential-row-label">{{ field.label }}</div>

      <!-- Configured: faux input row (visually identical to t-input) -->
      <template v-if="stateOf(field.key) === 'configured'">
        <!--
          Two possible looks:
          a) default — ✓ 已配置 + [更换 | 移除]
          b) confirm-remove pending — ⚠ 确认移除？此操作不可撤销 + [取消 | 确认移除]

          We use a sub-state instead of a global modal because the modal
          forces the user to context-switch to the screen center, then back
          to this row to see the result. Inline confirm keeps focus on the
          row that's actually changing.
        -->
        <div
          class="credential-faux-input"
          :class="{ 'is-confirm-remove': pendingRemove[field.key] }"
          :title="pendingRemove[field.key] ? '' : t('credential.configured')"
        >
          <template v-if="pendingRemove[field.key]">

            <span class="credential-faux-text danger">{{ t('credential.confirmRemovePrompt') }}</span>
            <div class="credential-actions">
              <Button variant="ghost" @click="cancelPendingRemove(field.key)">
                {{ t('common.cancel') }}
              </Button>
              <span class="action-divider"></span>
              <Button variant="danger" :loading="busy[field.key] === 'remove'"
                @click="confirmRemove(field)">
                {{ t('credential.confirmRemove') }}
              </Button>
            </div>
          </template>
          <template v-else>

            <span class="credential-faux-text">{{ t('credential.configured') }}</span>
            <div class="credential-actions">
              <Button variant="ghost" @click="enterEdit(field.key)">
                {{ t('credential.update') }}
              </Button>
              <span class="action-divider"></span>
              <Button variant="danger" @click="requestRemove(field.key)">
                {{ t('credential.remove') }}
              </Button>
            </div>
          </template>
        </div>
      </template>

      <!-- Unconfigured: faux input row with a single "Configure" affordance -->
      <template v-else-if="stateOf(field.key) === 'unconfigured'">
        <div
          class="credential-faux-input is-empty"
          :class="{ 'is-just-removed': inlineToast[field.key]?.kind === 'removed' }"
          @click="enterEdit(field.key)"
        >
          <!--
            Right after a successful remove we hold the row in place but swap
            the icon + placeholder text for a brief success state. After
            ~2.4s the row fades back to its plain "未配置" prompt. This
            keeps feedback anchored to where the user just clicked, instead
            of asking them to glance at a global toast somewhere else.
          -->
          <template v-if="inlineToast[field.key]?.kind === 'removed'">

            <span class="credential-faux-text">{{ t('credential.removedToast') }}</span>
          </template>
          <template v-else>

            <span class="credential-faux-text muted">{{ t('credential.unconfigured') }}</span>
            <div class="credential-actions">
              <Button variant="ghost" @click.stop="enterEdit(field.key)">
                {{ t('credential.configure') }}
              </Button>
            </div>
          </template>
        </div>
      </template>

      <!-- Editing: real input + tiny action row beneath -->
      <template v-else>
        <div class="credential-edit">
          <Input v-model="drafts[field.key]" :aria-label="field.label" type="password"
            :placeholder="field.placeholder ?? t('credential.inputPlaceholder')" :autocomplete="'new-password'"
            class="credential-edit-input" @keydown.enter="!$event.isComposing && onSave(field)">

          </Input>
          <div class="credential-edit-actions">
            <Button variant="ghost" @click="cancelEdit(field.key)">
              {{ t('common.cancel') }}
            </Button>
            <Button :loading="busy[field.key] === 'save'" :disabled="!drafts[field.key]"
              @click="onSave(field)">
              {{ t('common.save') }}
            </Button>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts" generic="K extends string">
import { Button, Input, toast, confirm } from '@/components/ui'
import '@/styles/settings-form.css'
import { onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export interface CredentialFieldDef<K extends string = string> {
  key: K
  label: string
  // Optional connector-specific placeholder shown only when the input is
  placeholder?: string
}

export interface CredentialResourceApi<K extends string = string> {
  // PUT /credentials — body keyed by field name, value is the new secret.
  // Returns the updated per-field configured map.
  save: (patch: Partial<Record<K, string>>) => Promise<Record<K, { configured: boolean }>>
  // DELETE /credentials/:field
  remove: (field: K) => Promise<void>
}

interface Props {
  fields: CredentialFieldDef<K>[]
  api: CredentialResourceApi<K>
  // Initial per-field "configured?" map, sourced from the parent resource's
  // main GET response. The component reads it on first render and after
  // every reset; subsequent state transitions are tracked locally.
  meta: Record<K, { configured: boolean }>
}

interface Emits {
  // Fires after every successful save or remove so the parent can refresh
  // any derived view (e.g. badges that depend on credential state) or just
  // reload the main resource to keep `meta` in sync.
  (e: 'changed'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()
const { t } = useI18n()

type State = 'configured' | 'unconfigured' | 'editing'
// Local view state per field. Source of truth is props.meta, but we track
// the editing state and any locally-applied save/remove transitions here so
// the UI doesn't snap back when the parent re-renders before re-fetching.
const states = reactive<Record<string, State>>({})
const drafts = reactive<Record<string, string>>({})
const busy = reactive<Record<string, 'save' | 'remove' | null>>({})

// Per-field "did the user just press Remove" flag. While true, the
// configured row swaps to an inline confirm prompt instead of immediately
// deleting. Cleared on cancel, on the actual DELETE request firing, and
// any time the field leaves the configured state for any other reason.
const pendingRemove = reactive<Record<string, boolean>>({})

// Inline per-field flash message used as an anchored "toast" instead of the
// global MessagePlugin one for actions whose effect happens at this exact
// row (currently: remove). Cleared after a short delay so the row reverts
// to its normal placeholder. Keyed by field.key.
type InlineToastKind = 'removed'
const inlineToast = reactive<Record<string, { kind: InlineToastKind } | null>>({})
const inlineToastTimers: Record<string, ReturnType<typeof setTimeout> | null> = {}

function flashInlineToast(key: string, kind: InlineToastKind, ms = 2400) {
  inlineToast[key] = { kind }
  if (inlineToastTimers[key]) {
    clearTimeout(inlineToastTimers[key]!)
  }
  inlineToastTimers[key] = setTimeout(() => {
    inlineToast[key] = null
    inlineToastTimers[key] = null
  }, ms)
}

function deriveStatesFromMeta(meta: Record<string, { configured: boolean }>) {
  for (const f of props.fields) {
    // Preserve in-progress edits across parent re-renders — `meta` describes
    // server state, the editing flag is user intent.
    if (states[f.key] === 'editing') continue
    states[f.key] = meta[f.key]?.configured ? 'configured' : 'unconfigured'
  }
}

// Initialize from the first meta snapshot, and re-derive whenever the parent
// passes a new one (after a main-resource refresh). watch with immediate:true
// covers both cases in one place.
watch(
  () => props.meta,
  (m) => deriveStatesFromMeta(m ?? ({} as Record<K, { configured: boolean }>)),
  { immediate: true, deep: true },
)

// If the parent swaps the api (e.g. user opens a different resource), drop
// transient state. props.meta will follow and re-init via the watch above.
watch(() => props.api, () => {
  for (const k of Object.keys(states)) delete states[k]
  for (const k of Object.keys(drafts)) delete drafts[k]
  for (const k of Object.keys(pendingRemove)) delete pendingRemove[k]
})

function stateOf(key: string): State {
  return states[key] ?? 'unconfigured'
}

function enterEdit(key: string) {
  drafts[key] = ''
  states[key] = 'editing'
  // If the user was mid-confirm and changed their mind ("update" instead
  // of "remove"), drop the pending flag so we don't bounce back into the
  // confirm UI when they cancel the edit.
  pendingRemove[key] = false
}

// Cancel returns directly to whatever the parent told us via props.meta —
// no async re-fetch needed, and no risk of staying stuck in 'editing'
// because the previous implementation's refresh was a no-op when state
// was already 'editing'.
function cancelEdit(key: string) {
  drafts[key] = ''
  states[key] = props.meta?.[key as K]?.configured ? 'configured' : 'unconfigured'
}

async function onSave(field: CredentialFieldDef) {
  const value = drafts[field.key]
  if (!value) return
  busy[field.key] = 'save'
  try {
    // Apply the save's returned metadata locally so the card flips to
    // 'configured' immediately. Skip the editing-preserve guard since this
    // particular field just finished editing.
    const updated = await props.api.save({ [field.key]: value } as Partial<Record<K, string>>)
    for (const f of props.fields) {
      if (f.key === field.key) continue
      if (states[f.key] === 'editing') continue
      states[f.key] = updated[f.key as K]?.configured ? 'configured' : 'unconfigured'
    }
    states[field.key] = updated[field.key as K]?.configured ? 'configured' : 'unconfigured'
    drafts[field.key] = ''
    toast.success(t('credential.savedToast'))
    emit('changed')
  } catch (err: any) {
    toast.error(err?.message || t('credential.saveFailed'))
  } finally {
    busy[field.key] = null
  }
}

// Two-step remove. First click flips the row to a confirm state; second
// click actually fires DELETE. We deliberately do NOT use a global modal
// here — the row is also the place where the result will appear, and a
// modal forces an unnecessary screen-center detour. The danger-themed
// "确认移除" button on a tinted-warning row gives the same protection
// against fat-fingered destructive clicks without that detour.
//
// Errors still surface via global MessagePlugin so the user can't miss them.
function requestRemove(key: string) {
  pendingRemove[key] = true
}

function cancelPendingRemove(key: string) {
  pendingRemove[key] = false
}

async function confirmRemove(field: CredentialFieldDef) {
  busy[field.key] = 'remove'
  try {
    await props.api.remove(field.key as K)
    states[field.key] = 'unconfigured'
    pendingRemove[field.key] = false
    flashInlineToast(field.key, 'removed')
    emit('changed')
  } catch (err: any) {
    toast.error(err?.message || t('credential.removeFailed'))
  } finally {
    busy[field.key] = null
  }
}

// Cancel pending inline-toast timers when the component unmounts so we
// don't write to a torn-down reactive object after navigation.
onBeforeUnmount(() => {
  for (const k of Object.keys(inlineToastTimers)) {
    if (inlineToastTimers[k]) {
      clearTimeout(inlineToastTimers[k]!)
      inlineToastTimers[k] = null
    }
  }
})
</script>

<style scoped>
 .credential-resource { width: 100%; }
.credential-faux-input, .credential-actions, .credential-edit-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.credential-faux-input { border: 1px solid var(--border); padding: var(--space-2); }
.credential-faux-text { flex: 1; overflow-wrap: anywhere; }
.credential-edit { display: grid; gap: var(--space-2); }
.credential-edit-actions { justify-content: flex-end; }
.is-confirm-remove { color: var(--danger); background: var(--danger-soft); }

</style>
