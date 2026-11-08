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
// identifiers have no exceptions; Library and ordinary words are excluded.
type entryNamingException struct {
	Reason string
	Tokens []string
}

var entryNamingExceptions = map[string]entryNamingException{
	"frontend/src/config/settingsNavigation.test.ts": {
		Reason: "验证旧设置弹窗请求中的 section 参数能导航到库设置页",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/settings/ModelSettings.deletion.test.ts": {
		Reason: "模型删除测试使用既有 HTTP 模型类型枚举，保持已持久化取值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/views/settings/Settings.page.test.ts": {
		Reason: "验证旧设置书签 section 参数重定向到库设置的兼容行为",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/insights/ContentChecksPage.vue": {
		Reason: "自检模型筛选使用既有 HTTP 模型类型枚举，不改已持久化值",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/maintenance/library_schema_pg_test.go": {
		Reason: "核对冻结的五库迁移保全历史任务 payload，导入时转换",
		Tokens: []string{"knowledge_id"},
	},
	"internal/router/ikb22_entry_contract_pg_test.go": {
		Reason: "逐接口验证旧字段和工具拒绝，直接核对线上协议键",
		Tokens: []string{"knowledge", "knowledge_id", "knowledge_ids"},
	},
	"internal/maintenance/entry_contract_names_pg_test.go": {
		Reason: "验证旧副本任务与报告字段转换，用户 JSON 原值保全",
		Tokens: []string{"knowledge_id"},
	},
	"internal/maintenance/entry_contract_names.go": {
		Reason: "旧副本中的历史任务字段转换，新副本写 entry",
		Tokens: []string{"knowledge_id", "knowledge_ids"},
	},
	"internal/testutil/pgtest/library_migration_test.go": {
		Reason: "库改名迁移保留历史队列 payload 的条目键",
		Tokens: []string{"knowledge_id"},
	},
	"internal/types/library_payload_test.go": {
		Reason: "验证旧队列归属键可读且新写入使用库字段",
		Tokens: []string{"knowledge_id"},
	},
	"cmd/server/maintenance_sources_pg_test.go": {
		Reason: "冻结基线恢复的真实离线 CLI 顺序使用旧表夹具",
		Tokens: []string{"knowledges"},
	},
	"internal/maintenance/collection_migration_pg_test.go": {
		Reason: "冻结基线备份恢复到当前版本的真实旧表夹具",
		Tokens: []string{"knowledges"},
	},
	"internal/application/service/entry_reprocess_missing_pg_test.go": {
		Reason: "验证旧采集任务 payload 不再联网且保留旧文章状态",
		Tokens: []string{"knowledge_id"},
	},
	"internal/testutil/pgtest/ingest_migration_test.go": {
		Reason: "升级保全 fixture 的嵌套历史 JSON 键须逐字保留，不能随新迁移改名",
		Tokens: []string{"knowledge_id"},
	},
	"internal/application/service/entry_create.go": {
		Reason: "无标题来源的既有下载文件名格式，文件库名称保持不变",
		Tokens: []string{"Knowledge"},
	},
	"internal/application/repository/retriever/postgres/summary.go": {
		Reason: "检索摘要命中的既有合成 ID 前缀，保持返回结果完全一致",
		Tokens: []string{"knowledge"},
	},
	"internal/types/entry_summary.go": {
		Reason: "检索摘要命中的既有合成 ID 前缀，保持返回结果完全一致",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/config/settingsRoute.ts": {
		Reason: "旧设置书签的 section 参数映射到库设置",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/config/settingsRoute.test.ts": {
		Reason: "旧设置书签 section 参数的兼容断言",
		Tokens: []string{"knowledge"},
	},
	"internal/router/shelf_import_suggest_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/router/shelf_parsers_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/views/library/components/ShelfSettingsPage.vue": {
		Reason: "既有模型类型 KnowledgeQA 保持原值",
		Tokens: []string{"KnowledgeQA"},
	},
	"cmd/server/maintenance_credentials_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA"},
	},
	"cmd/server/maintenance_legacy_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"knowledge", "knowledge_id", "knowledges"},
	},
	"frontend/src/api/model-usage.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/insights/EvaluationPage.vue": {
		Reason: "测评生成模型筛选使用既有 HTTP 模型类型枚举，不改已持久化值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/views/insights/insights.test.ts": {
		Reason: "测评页面夹具核对既有 HTTP 模型类型枚举",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/api/model.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/api/personal-space-routes.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/components/GlobalCommandPalette/commands.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/components/ModelDebugDrawer.vue": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/components/ModelEditorDialog.vue": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/components/ModelSelector.vue": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/components/modelSelectorFilter.test.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/i18n/deletedFeatureCopy.test.ts": {
		Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge", "knowledgeList", "knowledgeStrategy"},
	},
	"frontend/src/i18n/libraryCopy.test.ts": {
		Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/i18n/locales/en-US.ts": {
		Reason: "历史空间说明与既有英文文案包含 knowledge 一词，不作为新标识符",
		Tokens: []string{"Knowledge", "knowledge"},
	},
	"frontend/src/i18n/locales/workspaceTerminology.test.ts": {
		Reason: "保持英文界面文案及旧 i18n 根键",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/router/routes.test.ts": {
		Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；旧书签查询参数读取和重定向断言；旧浏览器路径重定向",
		Tokens: []string{"knowledge", "knowledge_id"},
	},
	"frontend/src/utils/contextWindow.test.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/utils/contextWindow.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/utils/entryProcessingStatus.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/utils/modelTiers.test.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/utils/modelTiers.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/utils/requestAvailability.test.ts": {
		Reason: "旧 HTTP 路径的负面断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/utils/spaceModelReadiness.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/utils/thinkingControl.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/utils/workspaceCopy.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/utils/workspaceCopy.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/settings-and-auth-behavior.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/settings/ChatGPTSettings.test.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/views/settings/ChatGPTSettings.vue": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/views/settings/LibrarySettings.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/settings/ModelSettings.chatgpt.test.ts": {
		Reason: "已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/views/settings/ModelSettings.vue": {
		Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；已持久化模型类型值",
		Tokens: []string{"KnowledgeQA"},
	},
	"frontend/src/views/settings/modelUsageDetails.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/system/RuntimeQueues.embeddings.test.ts": {
		Reason: "现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"frontend/src/views/system/RuntimeQueues.vue": {
		Reason: "保留 i18n 键，两个 locale 的文案和键集合不变；现有 i18n 键、界面原文案或对应断言",
		Tokens: []string{"knowledge"},
	},
	"internal/application/repository/entry_span_repo_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge_processing"},
	},
	"internal/application/service/entry_article_processing_pg_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge"},
	},
	"internal/application/service/entry_span_tracker.go": {
		Reason: "处理 span 的持久 root 名称保持不变",
		Tokens: []string{"knowledge_processing"},
	},
	"internal/application/service/library_settings_json_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/container/maintenance_bootstrap_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/handler/entry.go": {
		Reason: "处理 trace 的持久名称保持不变，兼容既有 span",
		Tokens: []string{"knowledge_processing"},
	},
	"internal/handler/entry_spans_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge_processing"},
	},
	"internal/handler/model.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/maintenance/format.go": {
		Reason: "副本版本 1、2 的表名、外层列名和原校验和映射",
		Tokens: []string{
			"knowledge_entities",
			"knowledge_id",
			"knowledge_ids",
			"knowledge_processing_spans",
			"knowledge_repost_sources",
			"knowledge_topics",
			"knowledges",
		},
	},
	"internal/maintenance/maintenance_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/maintenance/policy_compare.go": {
		Reason: "核对已有模型类型的持久值",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/maintenance/policy_compare_pg_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/maintenance/schema_compat.go": {
		Reason: "识别版本 2 的旧条目表并在事务中临时适配",
		Tokens: []string{"knowledges"},
	},
	"internal/maintenanceops/state.go": {
		Reason: "离线维护按已有任务类型等待队列",
		Tokens: []string{"knowledge"},
	},
	"internal/queue/dead_letter_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge"},
	},
	"internal/router/mcp_pg_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge", "knowledge_id", "knowledge_ids"},
	},
	"internal/router/personal_auth_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/router/router_files_test.go": {
		Reason: "文件库路径的既有目录值保持不变，验证旧资源仍可访问",
		Tokens: []string{"knowledge"},
	},
	"internal/testutil/pgtest/entry_migration_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{
			"knowledge",
			"knowledge_entities",
			"knowledge_id",
			"knowledge_ids",
			"knowledge_processing",
			"knowledge_processing_spans",
			"knowledge_repost_sources",
			"knowledge_topics",
			"knowledges",
		},
	},
	"internal/testutil/pgtest/maintenance_migration_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge_entities", "knowledge_id", "knowledge_topics", "knowledges"},
	},
	"internal/testutil/pgtest/space_migration_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{
			"knowledge_entities",
			"knowledge_id",
			"knowledge_topics",
			"knowledges",
			"knowledges_summary_search_idx",
		},
	},
	"internal/types/builtin_models_config.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/types/builtin_models_config_test.go": {
		Reason: "既有模型类型 KnowledgeQA 保持原值；旧数据库维护 fixture 使用历史表与 payload",
		Tokens: []string{"KnowledgeQA", "knowledgeqa"},
	},
	"internal/types/entry_payload.go": {
		Reason: "旧任务键与新键双读，写入仍保留旧键",
		Tokens: []string{"knowledge_id", "knowledge_ids"},
	},
	"internal/types/entry_payload_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge", "knowledge_id", "knowledge_ids"},
	},
	"internal/types/model.go": {
		Reason: "已有模型类型 KnowledgeQA 的持久取值保持不变",
		Tokens: []string{"KnowledgeQA"},
	},
	"internal/types/resource.go": {
		Reason: "资源绑定的 owner_type 持久值保持不变",
		Tokens: []string{"knowledge"},
	},
	"internal/types/search.go": {
		Reason: "原检索目标类型取值保持不变，兼容已保存请求",
		Tokens: []string{"knowledge"},
	},
	"internal/types/task.go": {
		Reason: "队列类型取值和 payload 写入键保持不变",
		Tokens: []string{"knowledge", "knowledge_id", "knowledge_ids"},
	},
	"internal/types/task_dead_letter.go": {
		Reason: "死信持久 scope 取值保持不变",
		Tokens: []string{"knowledge"},
	},
	"internal/types/task_queue_test.go": {
		Reason: "固定原契约或持久值的兼容断言；历史迁移夹具使用对应旧结构",
		Tokens: []string{"knowledge"},
	},
	"internal/utils/presign_test.go": {
		Reason: "文件库路径的既有目录值保持不变，验证旧资源仍可访问",
		Tokens: []string{"knowledge"},
	},
}

func retiredEntryIdentifier(word string) bool {
	word = strings.ToLower(word)
	word = strings.ReplaceAll(word, "knowledgebase", "library")
	word = strings.ReplaceAll(word, "knowledge_base", "library")
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
			if relative == "internal/types/entry_naming_guard_test.go" || relative == "internal/types/library_naming_guard_test.go" {
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
				line = library.ReplaceAllString(line, "library")
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

// Only frozen migrations and explicit old-replica readers may mention retired
// contract keys in shipped code. Tests exercise rejection; snapshots are scanned.
func TestEntryContractNamingGuard(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	retired := regexp.MustCompile(`\b(article_ids?|latest_article_id|related_article_ids?|get_article|knowledge_ids?|article_count|recent_article_count|failed_articles)\b`)
	exceptions := map[string]string{
		"internal/maintenance/format.go":                             "Converts frozen replica formats 1 and 2 without traversing user JSON",
		"internal/maintenance/schema_rules.go":                       "Classifies frozen pre-23 schemas without upgrading them",
		"internal/maintenance/entry_contract_names.go":               "Converts frozen pre-23 columns in old replicas only",
		"internal/types/entry_payload.go":                            "Reads task envelopes retained by old backups; new writers use entry keys",
		"internal/application/service/vault_legacy_header.go":        "Converts old headers only after versioned replica validation",
		"migrations/versioned/000023_entry_contract_naming.up.sql":   "Renames pre-23 database columns",
		"migrations/versioned/000023_entry_contract_naming.down.sql": "Reverses names only for temporary migration tests",
	}
	for _, directory := range []string{"internal", "cmd", "frontend/src", "migrations/versioned"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, file fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if file.IsDir() {
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			relative = filepath.ToSlash(relative)
			if strings.HasSuffix(relative, "_test.go") || strings.Contains(relative, ".test.") {
				return nil
			}
			if strings.HasPrefix(relative, "migrations/versioned/") && len(file.Name()) >= 6 && file.Name()[:6] <= "000022" {
				// Already applied migration SQL is immutable historical evidence.
				return nil
			}
			if strings.Contains(relative, "/testdata/") && relative != "internal/mcpserver/testdata/entry-contract.json" {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".ts" && ext != ".vue" && ext != ".json" && ext != ".sql" {
				return nil
			}
			if exceptions[relative] != "" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(data), "\n") {
				if token := retired.FindString(line); token != "" {
					t.Errorf("%s:%d: retired entry contract name %s", relative, i+1, token)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
