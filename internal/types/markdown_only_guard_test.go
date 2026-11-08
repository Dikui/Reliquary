package types

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMarkdownOnlyCapabilityGuard(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	retired := regexp.MustCompile(`(?i)anydoc|mineru|duckdb|resolve_processing_review|convert_to_mine|/space/kv/parser-engine-config|/processing-review|/system/parser-engines|/entries/[^\s"` + "`" + `]*/(?:download|preview)`)
	localeTopKey := regexp.MustCompile(`^  ["']?(\w+)["']?: \{`)
	frontendRetired := regexp.MustCompile(`(?i)多模态文档解析|文档解析|解析引擎|原件|处理待复核|待复核|不确定|摘要加链接|转为本人文档|multimodalParsing|document[ -]+pars(?:e|ing)|parser[ -]+(?:engine|provider)|(?:download|preview)[ -]+original|original[ -]+(?:download|preview)|original[ -]+(?:file|document|attachment)|processing[ -]+review|uncertain|summary[ -]+only|summary[ -]+(?:and|with|plus)[ -]+link|convert[^\n]*personal[ -]+document|personal[ -]+document|\bconvert\w*\b`)
	// These exact lines describe retained personal-document ingest/deletion or
	// unrelated data conversions. Do not whitelist whole locale files: a new
	// conversion action must still fail this guard.
	// Uncertain entity identity is retained; it is not an uncertain content type.
	// The personal-document-delete CSS class belongs to the retained delete action.
	frontendAllowed := []string{
		`/** Convert a Markdown excerpt to text before escaping or highlighting it. */`,
		`* Convert the entry-level parse status to the span status vocabulary used`,
		`* Convert a visual-pixel measurement to the CSS-pixel value that should be`,
		`// Initial credential metadata. ModelSettings.convertToLegacyFormat`,
		`<t-button class="personal-document-delete" size="small" variant="text" theme="danger" :disabled="disabled || busy" @click="open">`,
		`function convertToLegacyFormat(model: ModelConfig) {`,
		`const allLegacyModels = computed(() => allModels.value.map(convertToLegacyFormat))`,
		`"incompleteHint": "Articles and personal documents are missing images. Accept the missing materials on the Pending page, or have an external tool complete them. Resubmit articles with the same original URL and personal documents with the document ID.",`,
		`"incompleteHint": "Articles and personal documents are saved and searchable, but images are missing. Resubmit articles with the same original URL and personal documents with the document ID. You can also accept the missing materials or discard the content.",`,
		`"emptyIncomplete": "No articles or personal documents need completion.",`,
		`"delete_document": "Delete personal document",`,
		`"description": "Uncertain entities stay separate. Check the article before merging into a candidate.",`,
		`sourceKinds: { mine: 'Personal document', report: 'Report' },`,
		`delete: 'Delete personal document',`,
		`deleted: 'Personal document deleted',`,
		`deleteFailed: 'The personal document was not deleted. Try again.',`,
		`mineImportedHint: 'Personal documents are imported through the ingest interface. Resubmit through the ingest interface to update the content.',`,
	}
	// Frozen historical schema retains names needed by previous installations.
	historical := map[string]bool{"migrations/versioned/000000_init.up.sql": true}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == ".scratch" || relative == "docs" || relative == "frontend/dist" || (strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".github") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(relative, "_test.go") || strings.HasSuffix(relative, ".test.ts") || strings.HasPrefix(relative, "scripts/test-") || strings.Contains(relative, "/testdata/") || historical[relative] {
			return nil
		}
		ext := filepath.Ext(relative)
		source := ext == ".go" || ext == ".ts" || ext == ".vue" || ext == ".py" || ext == ".sh" || ext == ".sql" || ext == ".yml" || ext == ".yaml" || relative == "go.mod" || strings.Contains(entry.Name(), "Dockerfile") || relative == "Makefile"
		if !source {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, hit := range retired.FindAllString(string(data), -1) {
			// This preview counts a batch selection; it does not expose a retired original-file preview.
			if relative == "internal/router/routes_entry.go" && hit == "/entries/batches/preview" {
				continue
			}
			t.Errorf("retired capability %q in %s", hit, relative)
		}
		// ADR 0024 restores originals, parsing and uncertain-item review for shelf libraries only.
		shelfFile := strings.Contains(strings.ToLower(filepath.Base(relative)), "shelf")
		if strings.HasPrefix(relative, "frontend/src/") && !shelfFile {
			shelfBlock := false
			for number, line := range strings.Split(string(data), "\n") {
				if top := localeTopKey.FindStringSubmatch(line); top != nil {
					shelfBlock = strings.Contains(strings.ToLower(top[1]), "shelf")
				}
				if shelfBlock {
					continue
				}
				// An unknown retry outcome is unrelated to retired content types.
				if !frontendRetired.MatchString(strings.ReplaceAll(line, "retryUncertain", "")) {
					continue
				}
				allowed := false
				for _, exception := range frontendAllowed {
					if strings.TrimSpace(line) == exception {
						allowed = true
						break
					}
				}
				if !allowed {
					t.Errorf("retired frontend copy in %s:%d: %s", relative, number+1, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
