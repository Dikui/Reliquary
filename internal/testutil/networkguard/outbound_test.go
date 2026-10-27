package networkguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Every request construction and transport call needs an explicit review.
// Counts also reject a second call added to an already reviewed function.
var allowedOutboundCalls = map[string]int{
	// Service requests and parser result transfers.
	"internal/chatgpt/http.go|request|http.NewRequestWithContext":                         1,
	"internal/chatgpt/http.go|request|m.client.Do":                                        1,
	"internal/models/chat/anthropic.go|ChatStream|http.NewRequestWithContext":             1,
	"internal/models/chat/anthropic.go|ChatStream|rawHTTPClient.Do":                       1,
	"internal/models/chat/anthropic.go|Chat|http.NewRequestWithContext":                   1,
	"internal/models/chat/anthropic.go|Chat|rawHTTPClient.Do":                             1,
	"internal/models/chat/chatgpt.go|open|c.client.Do":                                    1,
	"internal/models/chat/chatgpt.go|open|http.NewRequestWithContext":                     1,
	"internal/models/chat/remote_api.go|chatStreamWithRawHTTP|http.NewRequestWithContext": 1,
	"internal/models/chat/remote_api.go|chatStreamWithRawHTTP|rawHTTPClient.Do":           1,
	"internal/models/chat/remote_api.go|chatWithRawHTTP|http.NewRequestWithContext":       1,
	"internal/models/chat/remote_api.go|chatWithRawHTTP|rawHTTPClient.Do":                 1,
	"internal/models/embedding/openai.go|doRequestWithRetry|e.httpClient.Do":              1,
	"internal/models/embedding/openai.go|doRequestWithRetry|http.NewRequestWithContext":   1,
	"internal/models/rerank/aliyun_reranker.go|rerank|http.NewRequestWithContext":         1,
	"internal/models/rerank/aliyun_reranker.go|rerank|r.client.Do":                        1,
	"internal/models/rerank/remote_api.go|Rerank|http.NewRequestWithContext":              1,
	"internal/models/rerank/remote_api.go|Rerank|r.client.Do":                             1,
	// Accounting delegates only to the existing guarded transport; no new endpoints.
	"internal/modelcalls/transport.go|RoundTrip|t.Inner.RoundTrip": 3,
	"internal/modelcalls/transport.go|RoundTrip|resp.Header.Get":   3,
	"internal/modelcalls/transport.go|finish|b.once.Do":            1,
	// Local worker initialization uses sync.Once and creates no network request.
	"internal/modelcalls/content.go|startContentWorker|contentWorker.Do": 1,
	// Shared guarded transports.
	"internal/utils/extraheaders.go|RoundTrip|base.RoundTrip":             2,
	"internal/utils/security.go|RoundTrip|t.Base.RoundTrip":               1,
	"internal/utils/security.go|roundTripPinnedProxy|transport.RoundTrip": 1,
	// Local selectors with HTTP-like method names.
	// Self-check Get calls read persisted run records, without making an HTTP request.
	"cmd/server/maintenance_chatgpt.go|runMaintenanceChatGPT|query.Get":          6,
	"internal/application/service/vault_file_rebuild.go|lockFileRebuild|once.Do": 1,
	// Prompt settings are persisted key/value reads, not outbound requests.
	"internal/handler/prompts.go|List|h.settings.Get":                                                  1,
	"internal/application/service/system_setting.go|Get|s.repo.Get":                                    1,
	"internal/application/service/system_setting.go|Update|s.repo.Get":                                 2,
	"internal/application/service/system_setting.go|resolveRaw|s.repo.Get":                             1,
	"internal/chatgpt/http.go|request|response.Header.Get":                                             2,
	"internal/handler/chatgpt_policy.go|Get|h.service.Get":                                             1,
	"internal/handler/entry.go|GetEntryBatch|c.Get":                                                    1,
	"internal/handler/entry.go|SearchEntry|c.Get":                                                      1,
	"internal/handler/entry.go|resolveEntryAndValidateLibraryAccess|c.Get":                             1,
	"internal/handler/entry.go|validateLibraryAccessWithLibraryID|c.Get":                               1,
	"internal/handler/library.go|validateAndGetLibrary|c.Get":                                          1,
	"internal/handler/model.go|DebugModel|c.PostForm":                                                  3,
	"internal/handler/system.go|GetSystemSetting|h.systemSettingSvc.Get":                               1,
	"internal/mcpserver/server.go|ServeHTTP|r.Header.Get":                                              1,
	"internal/middleware/error_handler.go|ErrorHandler|c.Writer.Header().Get":                          1,
	"internal/middleware/library_access.go|LibraryAccessFromContext|c.Get":                             1,
	"internal/middleware/logger.go|Logger|c.Get":                                                       1,
	"internal/middleware/logger.go|Logger|c.Writer.Header().Get":                                       1,
	"internal/middleware/recovery.go|Recovery|c.Get":                                                   1,
	"internal/models/chat/anthropic.go|Chat|resp.Header.Get":                                           1,
	"internal/models/chat/chatgpt.go|consume|resp.Header.Get":                                          1,
	"internal/models/chat/chatgpt.go|open|resp.Header.Get":                                             1,
	"internal/models/limiter/governor.go|acquire|once.Do":                                              1,
	"internal/utils/security.go|loadSSRFWhitelist|ssrfWhitelistOnce.Do":                                1,
	"internal/utils/ssrf_outbound_cache.go|validateURLForSSRFForOutbound|ssrfOutboundValidateGroup.Do": 1,
}

var candidateMethods = map[string]bool{
	"Get": true, "Head": true, "Post": true, "PostForm": true, "Do": true,
	"NewRequest": true, "NewRequestWithContext": true, "RoundTrip": true,
}

func scanCalls(path string, data []byte) (map[string]int, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, data, 0)
	if err != nil {
		return nil, err
	}
	found := map[string]int{}
	for _, declaration := range file.Decls {
		function := "<global>"
		if fn, ok := declaration.(*ast.FuncDecl); ok {
			function = fn.Name.Name
		}
		ast.Inspect(declaration, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !candidateMethods[selector.Sel.Name] {
				return true
			}
			var name bytes.Buffer
			_ = format.Node(&name, fileSet, selector)
			found[path+"|"+function+"|"+name.String()]++
			return true
		})
	}
	return found, nil
}

func TestProductionOutboundCallsMatchReviewedAllowlist(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	found := map[string]int{}
	for _, directory := range []string{"cmd", "internal", "third_party"} {
		// The optional vendored parser tree was removed for Markdown-only intake.
		if directory == "third_party" {
			if _, err := os.Stat(filepath.Join(root, directory)); os.IsNotExist(err) {
				continue
			}
		}
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			calls, err := scanCalls(filepath.ToSlash(relative), data)
			if err != nil {
				return err
			}
			for name, count := range calls {
				found[name] += count
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("RELIQUARY_PRINT_OUTBOUND_INVENTORY") == "1" {
		data, _ := json.MarshalIndent(found, "", "  ")
		fmt.Println(string(data))
	}
	for name, count := range found {
		if allowedOutboundCalls[name] != count {
			t.Errorf("unreviewed call or count: %s = %d, reviewed %d", name, count, allowedOutboundCalls[name])
		}
	}
	for name, count := range allowedOutboundCalls {
		if found[name] != count {
			t.Errorf("stale reviewed call: %s = %d, actual %d", name, count, found[name])
		}
	}
}

func TestScannerDetectsRequestConstructorsAndAliasedClients(t *testing.T) {
	source := []byte(`package sample
 import h "net/http"
 func fetch(){ req,_:=h.NewRequest("GET","https://example.org",nil); renamed:=h.DefaultClient; renamed.Do(req); renamed.Get("https://example.org"); renamed.Post("https://example.org","text/plain",nil); renamed.Head("https://example.org"); renamed.PostForm("https://example.org",nil); h.DefaultTransport.RoundTrip(req) }
 `)
	calls, err := scanCalls("sample.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 7 {
		t.Fatalf("scanner missed a request call: %#v", calls)
	}
}

func TestNewContentRequestHasNoAllowlistPermission(t *testing.T) {
	source := []byte(`package service
 import "net/http"
 func newContentFetch(){http.Get("https://example.org/source")}`)
	calls, err := scanCalls("internal/application/service/entry_reprocess_missing.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("new content request was not detected: %#v", calls)
	}
	for name, count := range calls {
		if allowedOutboundCalls[name] == count {
			t.Fatalf("new content request unexpectedly permitted: %s", name)
		}
	}
}
