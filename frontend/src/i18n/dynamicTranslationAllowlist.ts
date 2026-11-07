import type { TranslationCall } from './translationCallAudit.ts'

export const DYNAMIC_TRANSLATION_ALLOWLIST = [
  { file: 'views/settings/ManagedEntitySettings.vue', expressions: ['`productShelf.kinds.${key}`', 'list.label'], reason: 'Shelf categories and list headings use the fixed shelf vocabulary.' },
  { file: 'views/library/components/ProductShelf.vue', expressions: ['`productShelf.${value}`', "shelfRelationLabel(record.relation.relation_type) || 'productShelf.associated'"], reason: 'Shelf tabs and known relation labels are restricted to the product shelf vocabulary.' },
  { file: 'views/settings/ManagedFieldSchema.vue', expressions: ['`managed.types.${type}`'], reason: 'Schema field types come from the fixed supported type list.' },
  { file: 'views/library/components/ManagedEntityEditor.vue', expressions: ['`operationLogs.types.${value}`'], reason: 'Operation names are guarded by the shared OPERATION_TYPES list.' },
  {
    "file": "views/settings/LibraryCatalogSettings.vue",
    "expressions": [
      "`library.contentTypes.${name}`",
      "`settingsPage.colors.${colorNames[index]}`",
      "`libraryEditor.chunking.strategies.${strategy}.label`",
      "`libraryEditor.chunking.languageOptions.${language}`"
    ],
    "reason": "Built-in types are guarded by CONTENT_TYPES. Colors, strategies and languages come from fixed local choices; custom type names remain plain text."
  },
  {
    "file": "components/GlobalCommandPalette.vue",
    "expressions": [
      "`library.contentTypes.${value}`",
      "`workbench.entities.types.${entity.entity_type}`"
    ],
    "reason": "Built-in material types are checked against CONTENT_TYPES; entity types use the existing API enum."
  },
  {
    "file": "views/library/components/FollowFeedItem.vue",
    "expressions": [
      "`library.contentTypes.${props.article.content_type}`",
      "`follows.targetTypes.${target.target_type}`"
    ],
    "reason": "Built-in content types are guarded by CONTENT_TYPES; follow target types use the existing entity/topic enum. Custom content types remain plain text."
  },
  {
    "file": "views/library/TopicManagement.vue",
    "expressions": [
      "`library.contentTypes.${value}`"
    ],
    "reason": "Built-in content types are guarded by CONTENT_TYPES; custom library type names remain plain text."
  },
  {
    "file": "views/library/SearchResult.vue",
    "expressions": [
      "`library.contentTypes.${props.article.content_type}`",
      "`search.sources.${article.source}`"
    ],
    "reason": "Built-in content types are guarded by CONTENT_TYPES; search sources use the existing HTTP enum. Custom content types render as plain text."
  },
  {
    "file": "views/library/EntryDetail.vue",
    "expressions": [
      "processing.label",
      "`workbench.entities.types.${entity.entity_type}`"
    ],
    "reason": "Shared processing status keys and the fixed entity type vocabulary."
  },
  {
    "file": "views/library/components/DocumentListView.vue",
    "expressions": [
      "`entries.columns.${key}`",
      "entryProcessingState(item).label",
      "entryProcessingState(row).label"
    ],
    "reason": "The fixed material columns and shared processing state labels exist in both locales."
  },
  {
    "file": "views/library/components/EntryPreview.vue",
    "expressions": [
      "`processingStages.stage.${node.name}`",
      "`processingStages.status.${status}`",
      "state.label"
    ],
    "reason": "Preview stage names and statuses are restricted to the existing localized enums; entry state uses the shared resolver."
  },
  {
    "file": "views/insights/IngestFailuresPage.vue",
    "expressions": [
      "`ingestDiagnostics.${key}`"
    ],
    "reason": "Fixed failure table columns have labels in both locales."
  },
  {
    "file": "views/insights/FailedProcessingEntries.vue",
    "expressions": [
      "`library.contentTypes.${value}`",
      "entryProcessingState(row).label"
    ],
    "reason": "Built-in content types are guarded by CONTENT_TYPES; processing labels come from the shared state resolver."
  },
  {
    "file": "views/insights/ContentChecksPage.vue",
    "expressions": [
      "`contentCheck.day${value}`",
      "`contentCheck.${status}`",
      "`contentCheck.${group.kind}`"
    ],
    "reason": "Weekdays, run states and finding kinds use the fixed content-check contract and have labels in both locales."
  },
  {
    "file": "components/EntryBatchDialog.vue",
    "expressions": [
      "`entryBatch.actions.${value}`",
      "`entryBatch.actions.${batch.action}`",
      "`entryBatch.reasons.${reason.code}`",
      "`entryBatch.status.${batch.status}`",
      "`entryBatch.status.${item.status}`"
    ],
    "reason": "Batch actions, item states and preview reasons use the server batch contract; unknown reasons retain their raw code."
  },
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
    "file": "components/EmbeddingRebuild.vue",
    "expressions": [
      "`embeddingRebuild.kinds.${item.kind}`",
      "`embeddingRebuild.status.${run.status}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
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
    "file": "components/SearchFilters.vue",
    "expressions": [
      "`library.contentTypes.${value}`"
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
    "file": "views/library/FollowFeed.vue",
    "expressions": [
      "`follows.targetTypes.${target.target_type}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/library/Library.vue",
    "expressions": [
      "`search.sources.${value}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/library/OperationLogs.vue",
    "expressions": [
      "`operationLogs.types.${item.operation_type}`",
      "`operationLogs.types.${value}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/library/components/EntryWorkbench.vue",
    "expressions": [
      "`workbench.entities.types.${ENTITY_TYPES.includes(value as EntityType) ? value : 'other'}`",
      "`workbench.nameEmbeddings.status.${status}`"
    ],
    "reason": "Finite UI option or backend enum keys; existing prefix and mapped-key audits check the locale trees."
  },
  {
    "file": "views/library/components/MergeCandidate.vue",
    "expressions": [
      "`workbench.entities.types.${ENTITY_TYPES.includes(entityType.value as EntityType) ? entityType.value : 'other'}`"
    ],
    "reason": "Entity types are guarded by the fixed entity type vocabulary."
  },
  {
    "file": "views/library/components/PendingReviews.vue",
    "expressions": [
      "`library.contentTypes.${value}`"
    ],
    "reason": "Built-in content types are guarded by CONTENT_TYPES; custom names remain plain text."
  },
  {
    "file": "views/library/components/RepostSources.vue",
    "expressions": [
      "`workbench.sourceTime.matchMethods.${known ? method : 'other'}`"
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
    "file": "views/settings/ModelSettings.vue",
    "expressions": [
      "key",
      "modelUsageBindingI18nKey(binding)"
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
