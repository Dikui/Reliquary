import type { TranslationCall } from './translationCallAudit.ts'

/** Reviewed runtime expressions. New expressions must be explicitly reviewed here. */
export const DYNAMIC_TRANSLATION_ALLOWLIST = [
  {
    "file": "api/auth.ts",
    "expressions": [
      "key"
    ],
    "reason": "API helpers receive literal keys from their audited call sites."
  },
  {
    "file": "api/initialization.ts",
    "expressions": [
      "key"
    ],
    "reason": "API helpers receive literal keys from their audited call sites."
  },
  {
    "file": "api/model.ts",
    "expressions": [
      "key"
    ],
    "reason": "API helpers receive literal keys from their audited call sites."
  },
  {
    "file": "api/space.ts",
    "expressions": [
      "key"
    ],
    "reason": "API helpers receive literal keys from their audited call sites."
  },
  {
    "file": "components/ChatGPTUsageStatus.vue",
    "expressions": [
      "`chatgpt.pauseReasons.${key}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/DocContent.vue",
    "expressions": [
      "`knowledgeBase.sourceKinds.${props.details.source_kind}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/EmbeddingRebuild.vue",
    "expressions": [
      "`embeddingRebuild.kinds.${item.kind}`",
      "`embeddingRebuild.status.${run.status}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/EntryArticleJudgments.vue",
    "expressions": [
      "`knowledgeBase.contentTypes.${value}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/EntryClassification.vue",
    "expressions": [
      "`knowledgeBase.contentTypes.${type}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/EntryProcessingTimeline.vue",
    "expressions": [
      "'knowledgeStages.detail.' + detailTab",
      "`knowledgeStages.stage.${row.node.name}`",
      "`knowledgeStages.stage.${s.name}`",
      "`knowledgeStages.stage.${target.name}`",
      "`knowledgeStages.status.${s.status}`",
      "key"
    ],
    "reason": "Backend stage, status and error enums are covered by localeKeyAudit; detail tabs use a finite key set."
  },
  {
    "file": "components/KBInfoPopover.vue",
    "expressions": [
      "path"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/MissingMaterials.vue",
    "expressions": [
      "`ingestStatus.missingTypes.${item.type}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/ModelCallSummary.vue",
    "expressions": [
      "key"
    ],
    "reason": "Stage and status labels check te() and preserve the existing unknown-value fallback."
  },
  {
    "file": "components/ModelDebugDrawer.vue",
    "expressions": [
      "`modelSettings.typeShort.${keys[value].short}`",
      "key"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/ModelEditorDialog.vue",
    "expressions": [
      "`model.editor.providers.${p.value}.description`",
      "`model.editor.providers.${p.value}.label`",
      "`model.editor.thinkingControl.${key}.hint`",
      "`model.editor.thinkingControl.${key}.label`",
      "key"
    ],
    "reason": "Provider, thinking control and model type keys come from the editor option enums and have guarded fallbacks."
  },
  {
    "file": "components/ModelTierEditor.vue",
    "expressions": [
      "`modelTiers.names.${tier}`",
      "`modelTiers.uses.${tier}`",
      "key"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/ReviewActions.vue",
    "expressions": [
      "`knowledgeBase.contentTypes.${CONTENT_TYPES.includes(value as ContentType) ? value : 'unclassified'}`",
      "`knowledgeBase.contentTypes.${value}`",
      "`knowledgeReview.actions.${confirmation.value}`",
      "`knowledgeReview.confirmations.${confirmation.value}.description`",
      "`knowledgeReview.confirmations.${confirmation.value}.title`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/SearchEntry.vue",
    "expressions": [
      "`follows.targetTypes.${target.target_type}`",
      "`knowledgeSearch.sources.${article.source}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "components/SearchFilters.vue",
    "expressions": [
      "`knowledgeBase.contentTypes.${value}`",
      "`knowledgeSearch.sources.${value}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "i18n/countCopy.ts",
    "expressions": [
      "`countCopy.${noun}`",
      "key"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "utils/clipboard.ts",
    "expressions": [
      "successKey"
    ],
    "reason": "Shared helpers receive caller keys or map server errors to existing localized fallbacks."
  },
  {
    "file": "utils/mermaidViewer.ts",
    "expressions": [
      "key"
    ],
    "reason": "Shared helpers receive caller keys or map server errors to existing localized fallbacks."
  },
  {
    "file": "utils/personalServerError.ts",
    "expressions": [
      "fallback",
      "key"
    ],
    "reason": "Shared helpers receive caller keys or map server errors to existing localized fallbacks."
  },
  {
    "file": "utils/request.ts",
    "expressions": [
      "key"
    ],
    "reason": "Shared helpers receive caller keys or map server errors to existing localized fallbacks."
  },
  {
    "file": "views/knowledge-base/FollowFeed.vue",
    "expressions": [
      "`follows.targetTypes.${target.target_type}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/KnowledgeBase.vue",
    "expressions": [
      "`knowledgeBase.contentTypes.${value}`",
      "`knowledgeSearch.sources.${value}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/OperationLogs.vue",
    "expressions": [
      "`operationLogs.types.${item.operation_type}`",
      "`operationLogs.types.${value}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/TopicManagement.vue",
    "expressions": [
      "`topicManagement.status.${progress[topic.id]!.status}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/components/DocumentCardView.vue",
    "expressions": [
      "`knowledgeBase.sourceKinds.${item.source_kind}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/components/DocumentListView.vue",
    "expressions": [
      "`knowledgeBase.sourceKinds.${item.source_kind}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/components/EntryWorkbench.vue",
    "expressions": [
      "`knowledgeWorkbench.entities.types.${ENTITY_TYPES.includes(value as EntityType) ? value : 'other'}`",
      "`knowledgeWorkbench.nameEmbeddings.status.${status}`",
      "`knowledgeWorkbench.nav.${section}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/components/MergeSuggestions.vue",
    "expressions": [
      "`mergeSuggestions.empty.${status}`",
      "`mergeSuggestions.feedback.${action}`",
      "`mergeSuggestions.status.${item.status}`",
      "`mergeSuggestions.status.${value}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/components/PendingReviews.vue",
    "expressions": [
      "`ingestStatus.acquisition.${item.acquisition}`",
      "`knowledgeReview.${kind}Hint`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/components/RepostSources.vue",
    "expressions": [
      "`knowledgeWorkbench.sourceTime.matchMethods.${known ? method : 'other'}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/settings/KBChunkingDebug.vue",
    "expressions": [
      "`knowledgeEditor.chunking.debug.${p.labelKey}`",
      "`knowledgeEditor.chunking.strategies.${normalizeTier(tier)}.label`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/knowledge-base/settings/KBParserSettings.vue",
    "expressions": [
      "key"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/settings/GeneralSettings.vue",
    "expressions": [
      "`font.mono.${key}`",
      "`font.sans.${key}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/settings/KnowledgeBaseSettings.vue",
    "expressions": [
      "tab === 'parser' ? 'settings.parserEngine' : `knowledgeEditor.sidebar.${tab}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/settings/ModelSettings.vue",
    "expressions": [
      "key",
      "modelUsageBindingI18nKey(binding)"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/settings/ParserEngineSettings.vue",
    "expressions": [
      "key"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/settings/Settings.vue",
    "expressions": [
      "`integrations.tabs.${item.key}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/system/RuntimeQueues.vue",
    "expressions": [
      "`system.globalSettings.runtime.tasks.actionError.${action}`",
      "`system.globalSettings.runtime.tasks.actionSuccess.${action}`",
      "`system.globalSettings.runtime.tasks.guides.${taskState.value}`",
      "`system.globalSettings.runtime.tasks.states.${state}`",
      "path"
    ],
    "reason": "Settings sections, queue types and actions use backend enum maps and guarded translation lookups."
  },
  {
    "file": "views/system/SystemSettings.vue",
    "expressions": [
      "`${base}.body`",
      "`${base}.confirmBtn`",
      "`system.globalSettings.sections.${activeSettingsSection.value}.description`",
      "`system.globalSettings.sections.${activeSettingsSection.value}.title`",
      "`system.globalSettings.sections.${section}.tab`",
      "path"
    ],
    "reason": "Settings sections, queue types and actions use backend enum maps and guarded translation lookups."
  }
] as const

export function findUnlistedDynamicTranslationCalls(calls: TranslationCall[]): TranslationCall[] {
  const allowed = new Set(DYNAMIC_TRANSLATION_ALLOWLIST.flatMap(entry =>
    entry.expressions.map(expression => JSON.stringify([entry.file, expression])),
  ))
  return calls.filter(call => call.dynamic && !allowed.has(JSON.stringify([call.file, call.expression])))
}
