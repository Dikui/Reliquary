package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/service"
	"reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

func shelfParserSettings(f *managedNamesFixture, library *types.Library, config map[string]any) {
	f.t.Helper()
	f.request(http.MethodGet, "/libraries/"+library.ID, nil, http.StatusOK, library)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "settings", "type": "configure_shelf", "library_updated_at": library.UpdatedAt, "library": map[string]any{"shelf_config": config}}}}, http.StatusCreated, nil)
}

// Medium spec, task 09 / ADR 0024: the first matching rule wins; unmatched
// files use the default and none keeps a downloadable original without text.
func TestShelfParsersSelectFirstMatchingRule(t *testing.T) {
	f, library := newProductShelfFixture(t)
	config := map[string]any{"parsers": []any{
		map[string]any{"key": "first", "name": " First parser ", "type": "none"},
		map[string]any{"key": "second", "name": "Second parser", "type": "none"},
		map[string]any{"key": "fallback", "name": "Fallback parser", "type": "none"},
	}, "default_parser": "fallback", "parser_rules": []any{
		map[string]any{"parser_key": "first", "extension": ".PDF"},
		map[string]any{"parser_key": "second", "extension": ".pdf"},
	}}
	shelfParserSettings(f, &library, config)
	attachment := shelfUpload(t, f, library.ID, "Synthetic original", http.StatusCreated)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "owner", "type": "create_entity", "entity": map[string]any{"kind": "model", "name": "Parser owner"}},
		map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Parser file", "owner_id": "$owner", "attachment_id": attachment.ID, "stage": "final"}},
	}}, http.StatusCreated, &draft)
	var versions []map[string]any
	f.request(http.MethodGet, "/entities/"+shelfNode(draft, "file")+"/document-versions", nil, http.StatusOK, &versions)
	require.Equal(t, "first", versions[0]["parser_key"])
	require.Equal(t, "not_parsed", versions[0]["parse_status"])
	require.NotContains(t, versions[0], "entry_id")
	f.request(http.MethodGet, "/libraries/"+library.ID, nil, http.StatusOK, &library)
}

func shelfParserDocument(t *testing.T, f *managedNamesFixture, library types.Library, names ...string) (string, string) {
	suffix := ""
	if len(names) > 0 {
		suffix = names[0]
	}
	t.Helper()
	attachment := shelfUpload(t, f, library.ID, "Synthetic parser original", http.StatusCreated)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "owner", "type": "create_entity", "entity": map[string]any{"kind": "model", "name": "HTTP parser owner" + suffix}},
		map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "HTTP parser file" + suffix, "owner_id": "$owner", "attachment_id": attachment.ID, "stage": "final"}},
	}}, http.StatusCreated, &draft)
	var versions []types.ShelfDocumentVersion
	fileID := shelfNode(draft, "file")
	versions = nil
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	return fileID, versions[0].ID
}

func shelfParserWorker(t *testing.T, f *managedNamesFixture) <-chan error {
	t.Helper()
	client := queue.NewClientWithRedis(f.db, asynq.RedisClientOpt{Addr: os.Getenv("RELIQUARY_TEST_REDIS_ADDR")})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	finished := make(chan error, 16)
	mux := queue.NewServeMux()
	mux.HandleFunc(types.TypeManualProcess, f.articles.(interfaces.EntryService).ProcessManualUpdate)
	mux.HandleFunc(types.TypeShelfParse, func(ctx context.Context, task *queue.Task) error {
		err := f.entities.HandleShelfParse(ctx, task)
		if err != nil {
			t.Logf("parser worker: %v", err)
		}
		finished <- err
		return err
	})
	require.NoError(t, client.Start(context.Background(), mux, queue.Config{
		Pools: types.WorkerPoolConcurrency{Core: 2, Enrichment: 1, Maintenance: 1, Shared: 1}, CheckInterval: 20 * time.Millisecond,
		RetryDelayFunc: func(int, error, *queue.Task) time.Duration { return 10 * time.Millisecond },
	}))
	return finished
}

// Medium spec, task 09: a real durable worker sends the original to an HTTP
// service and publishes page-marked Markdown through the ordinary body API.
// httptest replaces only the externally configured parser service.
func TestShelfParsersHTTPProducesVersionText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-token" {
			http.Error(w, "auth", 401)
			return
		}
		original, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file", 400)
			return
		}
		defer original.Close()
		body, _ := io.ReadAll(original)
		if string(body) != "Synthetic parser original" || header.Filename != "synthetic-manual.pdf" {
			http.Error(w, "original", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"pages":[{"page":1,"markdown":"# Synthetic manual"},{"page":2,"markdown":"Rated power: 10 kW"}]}`)
	}))
	defer server.Close()
	f, library := newProductShelfFixture(t)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{map[string]any{"key": "external", "name": "External service", "type": "http", "url": server.URL, "authorization": "Bearer synthetic-token", "timeout_seconds": 5, "concurrency": 1}}, "default_parser": "external"})
	fileID, _ := shelfParserDocument(t, f, library)
	shelfParserWorker(t, f)
	var versions []types.ShelfDocumentVersion
	require.Eventually(t, func() bool {
		versions = nil
		f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
		return versions[0].ParseStatus == "completed"
	}, 15*time.Second, 30*time.Millisecond)
	require.Len(t, versions, 1)
	require.NotNil(t, versions[0].EntryID)
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries/"+*versions[0].EntryID+"/body", "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "<!-- page: 1 -->")
	require.Contains(t, response.Body.String(), "<!-- page: 2 -->")
	require.Contains(t, response.Body.String(), "Rated power: 10 kW")
	var links []service.ManagedEntityLink
	f.request(http.MethodGet, "/entries/"+*versions[0].EntryID+"/entity-links", nil, http.StatusOK, &links)
	require.Len(t, links, 2)
	for _, link := range links {
		require.Equal(t, "manual", link.Origin)
	}
}

// Medium spec, task 09: exhausting durable retries leaves the sole version
// failed, with no text and with its original still downloadable.
func TestShelfParsersExhaustedRetriesRetainOriginal(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "temporary parser failure", 503)
	}))
	defer server.Close()
	f, library := newProductShelfFixture(t)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{map[string]any{"key": "broken", "name": "Unavailable service", "type": "http", "url": server.URL, "timeout_seconds": 1, "concurrency": 1}}, "default_parser": "broken"})
	fileID, versionID := shelfParserDocument(t, f, library)
	shelfParserWorker(t, f)
	var versions []types.ShelfDocumentVersion
	require.Eventually(t, func() bool {
		versions = nil
		f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
		return versions[0].ParseStatus == "failed"
	}, 15*time.Second, 30*time.Millisecond)
	require.Greater(t, attempts.Load(), int32(1), "an exhausted parse must have retried the external service")
	require.Len(t, versions, 1)
	require.Nil(t, versions[0].EntryID)
	require.Contains(t, versions[0].ParseError, "503")
	original := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities/"+fileID+"/document-versions/"+versionID+"/download", "", f.key, true)
	require.Equal(t, http.StatusOK, original.Code)
	require.Equal(t, "Synthetic parser original", original.Body.String())
}

// Medium spec, task 09: reparse is a confirmed changeset on the same version;
// changing the named parser never appends a version or replaces the original.
func TestShelfParsersReparseChangesParserWithoutNewVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"pages":[{"page":1,"markdown":"Reparsed manual"}]}`)
	}))
	defer server.Close()
	f, library := newProductShelfFixture(t)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{
		map[string]any{"key": "skip", "name": "Keep original", "type": "none"},
		map[string]any{"key": "working", "name": "Working service", "type": "http", "url": server.URL, "timeout_seconds": 5, "concurrency": 1},
	}, "default_parser": "skip"})
	fileID, versionID := shelfParserDocument(t, f, library)
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	path := "/libraries/" + library.ID + "/shelf/changesets"
	var draft types.ShelfChangeset
	f.request(http.MethodPost, path, map[string]any{"source": map[string]any{"type": "web"}, "operations": []any{map[string]any{
		"id": "parse", "type": "reparse_document", "entity_id": fileID, "revision": detail.Entity.Revision, "reparse": map[string]any{"version_id": versionID, "parser_key": "working"},
	}}}, http.StatusCreated, &draft)
	var versions []types.ShelfDocumentVersion
	versions = nil
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	require.Equal(t, "not_parsed", versions[0].ParseStatus)
	f.request(http.MethodPost, path+"/"+draft.ID+"/confirm", map[string]any{"revision": draft.Revision, "decisions": []any{map[string]any{"item_id": "parse", "action": "apply"}}}, http.StatusOK, &draft)
	shelfParserWorker(t, f)
	require.Eventually(t, func() bool {
		versions = nil
		f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
		return versions[0].ParseStatus == "completed"
	}, 15*time.Second, 30*time.Millisecond)
	require.Len(t, versions, 1)
	require.Equal(t, versionID, versions[0].ID)
	require.Equal(t, "working", versions[0].ParserKey)
	require.EqualValues(t, 1, versions[0].Ordinal)
	original := personalRequest(t, f.router, http.MethodGet, "/api/v1/entities/"+fileID+"/document-versions/"+versionID+"/download", "", f.key, true)
	require.Equal(t, "Synthetic parser original", original.Body.String())
	textID := *versions[0].EntryID
	for _, parserKey := range []string{"skip", "working"} {
		f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
		requestBody, err := json.Marshal(map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{
			"id": "again", "type": "reparse_document", "entity_id": fileID, "revision": detail.Entity.Revision, "reparse": map[string]any{"version_id": versionID, "parser_key": parserKey},
		}}})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			response := personalRequest(t, f.router, http.MethodPost, "/api/v1"+path, string(requestBody), f.key, true)
			if response.Code == http.StatusConflict {
				return false
			}
			require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
			return true
		}, 5*time.Second, 20*time.Millisecond)
		if parserKey == "skip" {
			versions = nil
			f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
			require.Nil(t, versions[0].EntryID)
			require.Equal(t, "not_parsed", versions[0].ParseStatus)
			f.request(http.MethodGet, "/entries/"+textID+"/body", nil, http.StatusNotFound, nil)
		} else {
			require.Eventually(t, func() bool {
				versions = nil
				f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
				return versions[0].ParseStatus == "completed"
			}, 15*time.Second, 30*time.Millisecond)
			require.Equal(t, textID, *versions[0].EntryID)
		}
		require.Len(t, versions, 1)
		require.Equal(t, versionID, versions[0].ID)
	}

}

// A tiny synthetic PDF with a real cross-reference table, not customer data.
func shelfSyntheticPDF() string {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>`,
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
	}
	content := "BT /F1 12 Tf 20 150 Td (Synthetic manual) Tj ET\n"
	objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 6\n0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.String()
}

// Medium spec, task 09: the actual PDF renderer and model HTTP adapter send
// the configured prompt and page image, then publish page-marked model text.
// The local model server substitutes the external vision model only.
func TestShelfParsersVisionRendersPDFToVersionText(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("vision parsing needs poppler pdftoppm and pdfinfo")
	}
	f, library := newProductShelfFixture(t)
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(func() { utils.SetSSRFWhitelistFromRaw("") })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("Read the synthetic specification")) || !bytes.Contains(body, []byte("data:image/")) {
			http.Error(w, "missing prompt or rendered page", 400)
			return
		}
		match := regexp.MustCompile(`data:image/[^;]+;base64,([A-Za-z0-9+/=]+)`).FindSubmatch(body)
		if len(match) != 2 {
			http.Error(w, "missing rendered image", 400)
			return
		}
		data, _ := base64.StdEncoding.DecodeString(string(match[1]))
		dimensions, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || dimensions.Width != 400 || dimensions.Height != 400 {
			http.Error(w, "render DPI was ignored", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"synthetic-completion","choices":[{"index":0,"message":{"role":"assistant","content":"# Visual manual\nRated power: 20 kW"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`)
	}))
	defer server.Close()
	var model types.Model
	f.request(http.MethodPost, "/models", map[string]any{"name": "Synthetic vision model", "type": "KnowledgeQA", "source": "remote", "parameters": map[string]any{"base_url": server.URL + "/v1", "api_key": "synthetic-key", "supports_vision": true}}, http.StatusCreated, &model)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{map[string]any{"key": "vision", "name": "Visual reader", "type": "vision", "model_id": model.ID, "render_dpi": 144, "prompt": "Read the synthetic specification"}}, "default_parser": "vision"})
	attachment := shelfUpload(t, f, library.ID, shelfSyntheticPDF(), http.StatusCreated)
	var draft types.ShelfChangeset
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{
		map[string]any{"id": "owner", "type": "create_entity", "entity": map[string]any{"kind": "model", "name": "Visual model owner"}},
		map[string]any{"id": "file", "type": "add_document_version", "document": map[string]any{"name": "Visual manual", "owner_id": "$owner", "attachment_id": attachment.ID, "stage": "final"}},
	}}, http.StatusCreated, &draft)
	fileID := shelfNode(draft, "file")
	shelfParserWorker(t, f)
	var versions []types.ShelfDocumentVersion
	require.Eventually(t, func() bool {
		versions = nil
		f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
		return versions[0].ParseStatus == "completed"
	}, 15*time.Second, 30*time.Millisecond)
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries/"+*versions[0].EntryID+"/body", "", f.key, true)
	require.Contains(t, response.Body.String(), "<!-- page: 1 -->")
	require.Contains(t, response.Body.String(), "Rated power: 20 kW")
}

// Medium spec, task 09: unknown requirements, unavailable vision models and
// invalid service settings must be corrected before settings can be confirmed.
func TestShelfParsersRejectInvalidConfiguration(t *testing.T) {
	f, library := newProductShelfFixture(t)
	none := map[string]any{"key": "skip", "name": "Keep original", "type": "none"}
	for _, tc := range []struct {
		name   string
		config map[string]any
	}{
		{"unknown requirement", map[string]any{"parsers": []any{none}, "default_parser": "skip", "parser_rules": []any{map[string]any{"parser_key": "skip", "requirement_id": "unknown-requirement"}}}},
		{"unknown model", map[string]any{"parsers": []any{map[string]any{"key": "vision", "name": "Visual", "type": "vision", "model_id": "unknown-model", "render_dpi": 144, "prompt": "Read page"}}, "default_parser": "vision"}},
		{"unknown default", map[string]any{"parsers": []any{none}, "default_parser": "missing"}},
		{"unknown rule parser", map[string]any{"parsers": []any{none}, "default_parser": "skip", "parser_rules": []any{map[string]any{"parser_key": "missing", "extension": ".pdf"}}}},
		{"invalid concurrency", map[string]any{"parsers": []any{map[string]any{"key": "http", "name": "HTTP", "type": "http", "url": "http://localhost:7000/parse", "timeout_seconds": 5, "concurrency": 0}}, "default_parser": "http"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			f := &fixture
			f.request(http.MethodGet, "/libraries/"+library.ID, nil, http.StatusOK, &library)
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "settings", "type": "configure_shelf", "library_updated_at": library.UpdatedAt, "library": map[string]any{"shelf_config": tc.config}}}}, http.StatusConflict, nil)
		})
	}
}

// Medium regression, task 09: a configured visual parser protects its model
// from deletion through the existing library model-usage contract.
func TestShelfParsersVisionModelCannotBeDeleted(t *testing.T) {
	f, library := newProductShelfFixture(t)
	var model types.Model
	f.request(http.MethodPost, "/models", map[string]any{"name": "Referenced visual model", "type": "KnowledgeQA", "source": "remote", "parameters": map[string]any{"supports_vision": true}}, http.StatusCreated, &model)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{map[string]any{"key": "visual", "name": "Visual reader", "type": "vision", "model_id": model.ID, "render_dpi": 144, "prompt": "Read page"}}, "default_parser": "visual"})
	f.request(http.MethodDelete, "/models/"+model.ID, nil, http.StatusBadRequest, nil)
	f.request(http.MethodGet, "/models/"+model.ID, nil, http.StatusOK, nil)
}

// Medium regression, task 09: a late response from an older parser cannot
// publish text after a newer confirmed request selected none for that version.
func TestShelfParsersSupersededAttemptCannotPublishText(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, `{"pages":[{"page":1,"markdown":"Superseded text"}]}`)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f, library := newProductShelfFixture(t)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{
		map[string]any{"key": "slow", "name": "Slow service", "type": "http", "url": server.URL, "timeout_seconds": 5, "concurrency": 1},
		map[string]any{"key": "skip", "name": "Keep original", "type": "none"},
	}, "default_parser": "slow"})
	fileID, versionID := shelfParserDocument(t, f, library)
	finished := shelfParserWorker(t, f)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("external parser was not called")
	}
	var detail types.EntityDetail
	f.request(http.MethodGet, "/entities/"+fileID, nil, http.StatusOK, &detail)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "skip", "type": "reparse_document", "entity_id": fileID, "revision": detail.Entity.Revision, "reparse": map[string]any{"version_id": versionID, "parser_key": "skip"}}}}, http.StatusCreated, nil)
	close(release)
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("old parser did not finish")
	}
	var versions []types.ShelfDocumentVersion
	f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
	require.Len(t, versions, 1)
	require.Equal(t, "skip", versions[0].ParserKey)
	require.Equal(t, "not_parsed", versions[0].ParseStatus)
	require.Nil(t, versions[0].EntryID)
}

// Medium spec, task 09: selectors within a rule are conjunctive, requirement
// metadata supplies the category, and unmatched originals use the default.
func TestShelfParsersMatchSelectorsAndFallback(t *testing.T) {
	f, library := newProductShelfFixture(t)
	config := map[string]any{"parsers": []any{
		map[string]any{"key": "first", "name": "First reader", "type": "none"},
		map[string]any{"key": "second", "name": "Second reader", "type": "none"},
		map[string]any{"key": "fallback", "name": "Fallback reader", "type": "none"},
	}, "default_parser": "fallback", "stages": []any{map[string]any{"key": "finish", "name": "Synthetic finish"}}}
	shelfParserSettings(f, &library, config)
	var owner, req1, req2 types.Entity
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"name": "Rule owner", "kind": "model"}, http.StatusCreated, &owner)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"name": "Rule requirement one", "kind": "document_requirement", "attributes": map[string]any{"requirement": map[string]any{"list_name": "Synthetic list", "file_category": "Manual", "final_due_stage": "finish"}}}, http.StatusCreated, &req1)
	f.request(http.MethodPost, "/libraries/"+library.ID+"/entities", map[string]any{"name": "Rule requirement two", "kind": "document_requirement", "attributes": shelfFileRequirementAttributes(f, library)}, http.StatusCreated, &req2)
	config["parser_rules"] = []any{
		map[string]any{"parser_key": "first", "extension": ".PDF", "requirement_id": req1.ID, "file_category": "Manual"},
		map[string]any{"parser_key": "second", "file_category": "Manual"},
		map[string]any{"parser_key": "first", "requirement_id": req2.ID},
	}
	shelfParserSettings(f, &library, config)
	for i, tc := range []struct{ name, extension, requirement, category, want string }{
		{"all selectors", ".pdf", req1.ID, "Manual", "first"},
		{"extension differs", ".txt", req1.ID, "Manual", "second"},
		{"requirement differs", ".pdf", req2.ID, "Manual", "second"},
		{"category differs", ".pdf", req1.ID, "Other", "fallback"},
		{"requirement only", ".pdf", req2.ID, "Other", "first"},
		{"default", ".pdf", "", "Other", "fallback"},
		{"category from requirement", ".pdf", req1.ID, "", "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := *f
			fixture.t = t
			f := &fixture
			attachment := shelfUploadNamed(t, f, library.ID, "synthetic"+tc.extension, "Synthetic rule original", http.StatusCreated)
			var draft types.ShelfChangeset
			f.request(http.MethodPost, "/libraries/"+library.ID+"/shelf/changesets", map[string]any{"source": map[string]any{"type": "web"}, "confirmed": true, "operations": []any{map[string]any{"id": "document", "type": "add_document_version", "document": map[string]any{"name": fmt.Sprintf("Rule file %d", i), "owner_id": owner.ID, "requirement_id": tc.requirement, "file_category": tc.category, "attachment_id": attachment.ID, "stage": "final"}}}}, http.StatusCreated, &draft)
			var versions []types.ShelfDocumentVersion
			f.request(http.MethodGet, "/entities/"+draft.Items[0].EntityID+"/document-versions", nil, http.StatusOK, &versions)
			require.Equal(t, tc.want, versions[0].ParserKey)
			require.Equal(t, "not_parsed", versions[0].ParseStatus)
		})
	}
}

// Medium spec, task 09: concurrent version tasks obey the configured HTTP
// parser concurrency. The external stub observes in-flight requests only.
func TestShelfParsersHTTPHonorsConcurrency(t *testing.T) {
	var active, peak atomic.Int32
	started, release := make(chan struct{}, 2), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		io.WriteString(w, `{"pages":[{"page":1,"markdown":"Concurrent manual"}]}`)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f, library := newProductShelfFixture(t)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{map[string]any{"key": "limited", "name": "Limited reader", "type": "http", "url": server.URL, "timeout_seconds": 5, "concurrency": 1}}, "default_parser": "limited"})
	first, _ := shelfParserDocument(t, f, library, " first")
	second, _ := shelfParserDocument(t, f, library, " second")
	shelfParserWorker(t, f)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("parser did not start")
	}
	// Hold an actual request while the independent second task is dispatched.
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	for _, fileID := range []string{first, second} {
		require.Eventually(t, func() bool {
			var versions []types.ShelfDocumentVersion
			f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
			return versions[0].ParseStatus == "completed"
		}, 10*time.Second, 30*time.Millisecond)
	}
	require.EqualValues(t, 1, peak.Load())
}

// Medium spec, task 09: transient parser errors are retried by the durable
// queue and do not prevent the eventual version text from becoming readable.
func TestShelfParsersTransientFailureEventuallyPublishesText(t *testing.T) {
	var failures atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failures.Add(1) <= 2 {
			http.Error(w, "temporary outage", 503)
			return
		}
		io.WriteString(w, `{"pages":[{"page":1,"markdown":"Recovered parser text"}]}`)
	}))
	defer server.Close()
	f, library := newProductShelfFixture(t)
	shelfParserSettings(f, &library, map[string]any{"parsers": []any{map[string]any{"key": "retry", "name": "Retrying reader", "type": "http", "url": server.URL, "timeout_seconds": 5, "concurrency": 1}}, "default_parser": "retry"})
	fileID, _ := shelfParserDocument(t, f, library)
	shelfParserWorker(t, f)
	var versions []types.ShelfDocumentVersion
	require.Eventually(t, func() bool {
		versions = nil
		f.request(http.MethodGet, "/entities/"+fileID+"/document-versions", nil, http.StatusOK, &versions)
		return versions[0].ParseStatus == "completed"
	}, 15*time.Second, 30*time.Millisecond)
	require.Empty(t, versions[0].ParseError)
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries/"+*versions[0].EntryID+"/body", "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "Recovered parser text")
}
