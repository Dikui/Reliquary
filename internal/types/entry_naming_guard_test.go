package types

import (
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Exceptions are limited to the listed file and exact literal token. Go
// identifiers have no exceptions; KnowledgeBase and ordinary words are excluded.
type entryNamingException struct {
	Reason string
	Tokens []string
}

var entryNamingExceptions = map[string]entryNamingException{
	"cmd/server/maintenance_sources_pg_test.go":                                {Reason: "冻结基线恢复的真实离线 CLI 顺序使用旧表夹具", Tokens: []string{"knowledges"}},
	"internal/maintenance/collection_migration_pg_test.go":                     {Reason: "冻结基线备份恢复到当前版本的真实旧表夹具", Tokens: []string{"knowledges"}},
	"internal/application/service/entry_reprocess_missing_pg_test.go":          {Reason: "验证旧采集任务 payload 不再联网且保留旧文章状态", Tokens: []string{"knowledge_id"}},
	"internal/testutil/pgtest/ingest_migration_test.go":                        {Reason: "升级保全 fixture 的嵌套历史 JSON 键须逐字保留，不能随新迁移改名", Tokens: []string{"knowledge_id"}},
	"internal/application/service/entry_create.go":                             {Reason: "无标题来源的既有下载文件名格式，文件库名称保持不变", Tokens: []string{"Knowledge"}},
	"internal/application/service/vault_report.go":                             {Reason: "报告索引使用现有队列 payload 键，任务诊断和死信读取保持兼容", Tokens: []string{"knowledge_id"}},
	"internal/application/repository/retriever/postgres/summary.go":            {Reason: "检索摘要命中的既有合成 ID 前缀，保持返回结果完全一致", Tokens: []string{"knowledge"}},
	"internal/types/entry_summary.go":                                          {Reason: "检索摘要命中的既有合成 ID 前缀，保持返回结果完全一致", Tokens: []string{"knowledge"}},
	"frontend/src/config/settingsRoute.ts":                                     {Reason: "旧设置书签的 section 参数映射到知识库设置", Tokens: []string{"knowledge"}},
	"frontend/src/config/settingsRoute.test.ts":                                {Reason: "旧设置书签 section 参数的兼容断言", Tokens: []string{"knowledge"}},
	"cmd/server/maintenance_credentials_pg_test.go":                            {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA"}},
	"cmd/server/maintenance_legacy_pg_test.go":                                 {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"knowledge", "knowledge_id", "knowledges"}},
	"frontend/src/api/model-usage.test.ts":                                     {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/api/model.ts":                                                {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/api/personal-space-routes.test.ts":                           {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/components/DocContent.reposts.test.ts":                       {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview", "knowledgeWorkbench"}},
	"frontend/src/components/DocContent.vue":                                   {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview", "knowledgeStages"}},
	"frontend/src/components/EmptyEntry.vue":                                   {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"emptyKnowledgeDragDrop"}},
	"frontend/src/components/EntryClassification.vue":                          {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview"}},
	"frontend/src/components/EntryProcessingTimeline.vue":                      {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；既有处理 trace 阶段值", Tokens: []string{"knowledgeStages", "knowledge_processing"}},
	"frontend/src/components/GlobalCommandPalette.vue":                         {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/components/GlobalCommandPalette/ResultItem.vue":              {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview"}},
	"frontend/src/components/GlobalCommandPalette/commands.test.ts":            {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/components/GlobalCommandPalette/commands.ts":                 {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/components/KBInfoPopover.vue":                                {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor"}},
	"frontend/src/components/ManualEntryEditor.vue":                            {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeUnavailable"}},
	"frontend/src/components/ModelDebugDrawer.vue":                             {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/components/ModelEditorDialog.vue":                            {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/components/ModelSelector.vue":                                {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/components/ModelTierEditor.vue":                              {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor"}},
	"frontend/src/components/ReviewActions.vue":                                {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview"}},
	"frontend/src/components/SearchEntry.vue":                                  {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/components/SearchFilters.vue":                                {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/components/SearchImage.preview.test.ts":                      {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/components/SearchImage.test.ts":                              {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/components/SearchImage.vue":                                  {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/components/modelSelectorFilter.test.ts":                      {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/i18n/countMessages.test.ts":                                  {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor", "knowledgeWorkbench"}},
	"frontend/src/i18n/dynamicTranslationAllowlist.ts":                         {Reason: "动态文案审计逐字列出当前 i18n 表达式，不改既有文案键", Tokens: []string{"knowledgeEditor", "knowledgeReview", "knowledgeSearch", "knowledgeStages", "knowledgeWorkbench"}},
	"frontend/src/i18n/deletedFeatureCopy.test.ts":                             {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge", "knowledgeEditor", "knowledgeList", "knowledgeStrategy", "knowledgeWorkbench"}},
	"frontend/src/i18n/kbCopy.test.ts":                                         {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge", "knowledgeEditor", "knowledgeWorkbench"}},
	"frontend/src/i18n/localeKeyAudit.test.ts":                                 {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge", "knowledgeStages"}},
	"frontend/src/i18n/localeKeyAudit.ts":                                      {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeStages"}},
	"frontend/src/i18n/locales/en-US.ts":                                       {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；保持英文界面文案及旧 i18n 根键", Tokens: []string{"Knowledge", "emptyKnowledgeDragDrop", "knowledge", "knowledgeCountLabel", "knowledgeEditor", "knowledgeLabel", "knowledgeReview", "knowledgeSearch", "knowledgeSettings", "knowledgeStages", "knowledgeUnavailable", "knowledgeWorkbench", "noKnowledge", "returnToKnowledge"}},
	"frontend/src/i18n/locales/workspaceTerminology.test.ts":                   {Reason: "保持英文界面文案及旧 i18n 根键", Tokens: []string{"knowledge"}},
	"frontend/src/i18n/locales/zh-CN.ts":                                       {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；保持英文界面文案及旧 i18n 根键", Tokens: []string{"emptyKnowledgeDragDrop", "knowledge", "knowledgeCountLabel", "knowledgeEditor", "knowledgeLabel", "knowledgeReview", "knowledgeSearch", "knowledgeSettings", "knowledgeStages", "knowledgeUnavailable", "knowledgeWorkbench", "noKnowledge", "returnToKnowledge"}},
	"frontend/src/router/routes.test.ts":                                       {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；旧书签查询参数读取和重定向断言；旧浏览器路径重定向", Tokens: []string{"knowledge", "knowledge_id", "returnToKnowledge"}},
	"frontend/src/router/routes.ts":                                            {Reason: "旧书签查询参数读取和重定向断言；旧浏览器路径重定向", Tokens: []string{"knowledge", "knowledge_id"}},
	"frontend/src/stores/menu.ts":                                              {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeWorkbench"}},
	"frontend/src/utils/contextWindow.test.ts":                                 {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/utils/contextWindow.ts":                                      {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/utils/entryProcessingStatus.test.ts":                         {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/utils/fileTypeVerification.test.ts":                          {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"shouldRejectKnowledgeFileType"}},
	"frontend/src/utils/modelTiers.test.ts":                                    {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/utils/modelTiers.ts":                                         {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/utils/requestAvailability.test.ts":                           {Reason: "旧 HTTP 路径的负面断言", Tokens: []string{"knowledge"}},
	"frontend/src/utils/search.ts":                                             {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/utils/spaceModelReadiness.ts":                                {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/utils/thinkingControl.ts":                                    {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/utils/workspaceCopy.test.ts":                                 {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/utils/workspaceCopy.ts":                                      {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/views/NotFound.vue":                                          {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"returnToKnowledge"}},
	"frontend/src/views/integrations/ApiIntegrationSettings.expiry.test.ts":    {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/views/integrations/ApiIntegrationSettings.vue":               {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/views/knowledge-base/FollowFeed.test.ts":                     {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch", "knowledgeWorkbench"}},
	"frontend/src/views/knowledge-base/FollowFeed.vue":                         {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch", "knowledgeWorkbench"}},
	"frontend/src/views/knowledge-base/KnowledgeBase.vue":                      {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor", "knowledgeReview", "knowledgeWorkbench", "knowledgeSearch"}},
	"frontend/src/views/knowledge-base/KnowledgeBaseSourceFilter.test.ts":      {Reason: "复用检索页既有来源文案的 i18n 键", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/views/knowledge-base/Reviews.test.ts":                        {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview"}},
	"frontend/src/views/knowledge-base/Search.test.ts":                         {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview", "knowledgeSearch"}},
	"frontend/src/views/knowledge-base/SearchView.vue":                         {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSearch"}},
	"frontend/src/views/knowledge-base/components/DocumentActionMenu.vue":      {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeStages"}},
	"frontend/src/views/knowledge-base/components/DocumentCardView.vue":        {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeStages"}},
	"frontend/src/views/knowledge-base/components/EntryWorkbench.vue":          {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeWorkbench"}},
	"frontend/src/views/knowledge-base/components/MergeSuggestions.vue":        {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeWorkbench"}},
	"frontend/src/views/knowledge-base/components/PendingReviews.vue":          {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeReview", "knowledgeWorkbench"}},
	"frontend/src/views/knowledge-base/components/RepostSources.vue":           {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeWorkbench"}},
	"frontend/src/views/knowledge-base/components/UploadConfirmDialog.test.ts": {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/views/knowledge-base/components/UploadConfirmDialog.vue":     {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor", "knowledgeReview"}},
	"frontend/src/views/knowledge-base/settings/KBAdvancedSettings.vue":        {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor"}},
	"frontend/src/views/knowledge-base/settings/KBChunkingDebug.vue":           {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor"}},
	"frontend/src/views/knowledge-base/settings/KBChunkingSettings.vue":        {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor"}},
	"frontend/src/views/settings-and-auth-behavior.test.ts":                    {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/views/settings/ChatGPTSettings.test.ts":                      {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/views/settings/ChatGPTSettings.vue":                          {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/views/settings/KnowledgeBaseSettings.test.ts":                {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/views/settings/KnowledgeBaseSettings.vue":                    {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeEditor", "knowledgeSettings"}},
	"frontend/src/views/settings/ModelSettings.chatgpt.test.ts":                {Reason: "已持久化模型类型值", Tokens: []string{"KnowledgeQA"}},
	"frontend/src/views/settings/ModelSettings.vue":                            {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；已持久化模型类型值", Tokens: []string{"KnowledgeQA", "knowledgeSettings"}},
	"frontend/src/views/settings/Settings.vue":                                 {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变", Tokens: []string{"knowledgeSettings"}},
	"frontend/src/views/settings/modelUsageDetails.test.ts":                    {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge"}},
	"frontend/src/views/system/RuntimeQueues.embeddings.test.ts":               {Reason: "现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge", "knowledgeLabel"}},
	"frontend/src/views/system/RuntimeQueues.vue":                              {Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；现有 i18n 键、界面原文案或对应断言", Tokens: []string{"knowledge", "knowledgeCountLabel", "knowledgeLabel"}},
	"internal/application/repository/entry_span_repo_test.go":                  {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge_processing"}},
	"internal/application/service/entry_article_processing_pg_test.go":         {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge"}},
	"internal/application/service/entry_span_tracker.go":                       {Reason: "处理 span 的持久 root 名称保持不变", Tokens: []string{"knowledge_processing"}},
	"internal/application/service/knowledgebase_settings_json_pg_test.go":      {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA"}},
	"internal/application/service/operation_value.go":                          {Reason: "操作日志关联行继续写原内部 JSON 键", Tokens: []string{"knowledge_id"}},
	"internal/container/maintenance_bootstrap_pg_test.go":                      {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA"}},
	"internal/handler/entry.go":                                                {Reason: "处理 trace 的持久名称保持不变，兼容既有 span", Tokens: []string{"knowledge_processing"}},
	"internal/handler/entry_spans_test.go":                                     {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge_processing"}},
	"internal/handler/model.go":                                                {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA"}},
	"internal/maintenance/format.go":                                           {Reason: "副本版本 1、2 的表名、外层列名和原校验和映射", Tokens: []string{"knowledge_entities", "knowledge_id", "knowledge_ids", "knowledge_processing_spans", "knowledge_repost_sources", "knowledge_topics", "knowledges"}},
	"internal/maintenance/maintenance_pg_test.go":                              {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA"}},
	"internal/maintenance/policy_compare.go":                                   {Reason: "核对已有模型类型的持久值", Tokens: []string{"KnowledgeQA"}},
	"internal/maintenance/policy_compare_pg_test.go":                           {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA"}},
	"internal/maintenance/schema_compat.go":                                    {Reason: "识别版本 2 的旧条目表并在事务中临时适配", Tokens: []string{"knowledges"}},
	"internal/maintenanceops/state.go":                                         {Reason: "离线维护按已有任务类型等待队列", Tokens: []string{"knowledge"}},
	"internal/mcpserver/entry_contract_test.go":                                {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge_id"}},
	"internal/mcpserver/server.go":                                             {Reason: "MCP 输出适配原条目与关联行字段，任意 JSON 不转换", Tokens: []string{"knowledge", "knowledge_", "knowledge_id", "knowledge_ids"}},
	"internal/queue/dead_letter_test.go":                                       {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge"}},
	"internal/queue/worker.go":                                                 {Reason: "死信读取旧队列 payload 键", Tokens: []string{"knowledge_id"}},
	"internal/router/mcp_pg_test.go":                                           {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge", "knowledge_id", "knowledge_ids"}},
	"internal/router/personal_auth_test.go":                                    {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"KnowledgeQA"}},
	"internal/router/router_files_test.go":                                     {Reason: "文件库路径的既有目录值保持不变，验证旧资源仍可访问", Tokens: []string{"knowledge"}},
	"internal/router/task.go":                                                  {Reason: "死信回调双读旧任务 payload", Tokens: []string{"knowledge_id", "knowledge_ids"}},
	"internal/router/task_inspector.go":                                        {Reason: "旧任务诊断、取消与排队检查兼容原 payload 键", Tokens: []string{"knowledge_id", "knowledge_ids"}},
	"internal/testutil/pgtest/entry_migration_test.go":                         {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge", "knowledge_entities", "knowledge_id", "knowledge_ids", "knowledge_processing", "knowledge_processing_spans", "knowledge_repost_sources", "knowledge_topics", "knowledges"}},
	"internal/testutil/pgtest/maintenance_migration_test.go":                   {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge_entities", "knowledge_id", "knowledge_topics", "knowledges"}},
	"internal/testutil/pgtest/space_migration_test.go":                         {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge_entities", "knowledge_id", "knowledge_topics", "knowledges", "knowledges_summary_search_idx"}},
	"internal/types/builtin_models_config.go":                                  {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA"}},
	"internal/types/builtin_models_config_test.go":                             {Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload", Tokens: []string{"KnowledgeQA", "knowledgeqa"}},
	"internal/types/entry_payload.go":                                          {Reason: "旧任务键与新键双读，写入仍保留旧键", Tokens: []string{"knowledge_id", "knowledge_ids"}},
	"internal/types/entry_payload_test.go":                                     {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge", "knowledge_id", "knowledge_ids"}},
	"internal/types/model.go":                                                  {Reason: "已有模型类型 KnowledgeQA 的持久取值保持不变", Tokens: []string{"KnowledgeQA"}},
	"internal/types/resource.go":                                               {Reason: "资源绑定的 owner_type 持久值保持不变", Tokens: []string{"knowledge"}},
	"internal/types/search.go":                                                 {Reason: "原检索目标类型取值保持不变，兼容已保存请求", Tokens: []string{"knowledge"}},
	"internal/types/task.go":                                                   {Reason: "队列类型取值和 payload 写入键保持不变", Tokens: []string{"knowledge", "knowledge_id", "knowledge_ids"}},
	"internal/types/task_dead_letter.go":                                       {Reason: "死信持久 scope 取值保持不变", Tokens: []string{"knowledge"}},
	"internal/types/task_queue_test.go":                                        {Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构", Tokens: []string{"knowledge"}},
	"internal/utils/presign_test.go":                                           {Reason: "文件库路径的既有目录值保持不变，验证旧资源仍可访问", Tokens: []string{"knowledge"}},
}

func retiredEntryIdentifier(word string) bool {
	word = strings.ToLower(word)
	word = strings.ReplaceAll(word, "knowledgebase", "kb")
	word = strings.ReplaceAll(word, "knowledge_base", "kb")
	word = strings.ReplaceAll(word, "acknowledge", "ack")
	return strings.Contains(word, "knowledge")
}

func TestEntryNamingGuard(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	words := regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`)
	library := regexp.MustCompile(`(?i)knowledge[-_ ]bases?`)
	for _, directory := range []string{"internal", "cmd", "frontend/src"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			relative = filepath.ToSlash(relative)
			if relative == "internal/types/entry_naming_guard_test.go" {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".ts" && ext != ".vue" && ext != ".mjs" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if ext == ".go" {
				set := token.NewFileSet()
				file := set.AddFile(path, -1, len(data))
				var scan scanner.Scanner
				scan.Init(file, data, nil, 0)
				for {
					position, kind, literal := scan.Scan()
					if kind == token.EOF {
						break
					}
					if kind == token.IDENT && retiredEntryIdentifier(literal) {
						t.Errorf("%s: retired row identifier %s", set.Position(position), literal)
					}
				}
			}
			exception := entryNamingExceptions[relative]
			for index, line := range strings.Split(string(data), "\n") {
				line = library.ReplaceAllString(line, "kb")
				for _, word := range words.FindAllString(line, -1) {
					if !retiredEntryIdentifier(word) {
						continue
					}
					allowed := false
					for _, literal := range exception.Tokens {
						if word == literal {
							allowed = true
							break
						}
					}
					if !allowed || exception.Reason == "" {
						t.Errorf("%s:%d: retired token %s lacks a documented exception", relative, index+1, word)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
