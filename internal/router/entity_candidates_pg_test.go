package router

import (
	"context"
	"encoding/json"
	"net/http"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"

	"github.com/stretchr/testify/require"
)

type candidateFactWire struct {
	types.StoryFact
	SubjectEntityID *string `json:"subject_entity_id"`
	ObjectEntityID  *string `json:"object_entity_id"`
}

type candidateFixture struct {
	*mcpFixture
	model *understandingChat
}

func newCandidateFixture(t *testing.T) *candidateFixture {
	t.Helper()
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	return &candidateFixture{mcpFixture: f, model: model}
}

func (f *candidateFixture) request(t *testing.T, method, path string, input any, status int) []byte {
	t.Helper()
	body := ""
	if input != nil {
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		body = string(raw)
	}
	r := personalRequest(t, f.router, method, path, body, f.key, true)
	require.Equal(t, status, r.Code, r.Body.String())
	return r.Body.Bytes()
}

func (f *candidateFixture) entity(t *testing.T, name string, aliases ...string) types.Entity {
	t.Helper()
	var out struct{ Data types.Entity }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost, "/api/v1/entities", map[string]any{
		"canonical_name": name, "aliases": aliases,
	}, 201), &out))
	return out.Data
}

func (f *candidateFixture) process(t *testing.T, ref string, fact map[string]any) candidateFactWire {
	t.Helper()
	f.model.respond = func(_ context.Context, _ []chat.Message) string {
		raw, err := json.Marshal(
			map[string]any{"title": "实体挂接资料", "summary": map[string]string{"overview": "报道一次合作。"}, "fact": fact},
		)
		require.NoError(t, err)
		return string(raw)
	}
	result := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "official", "source_ref": ref, "title": ref,
		"content": "A collaboration was announced.", "published_at": "2026-10-24",
	})[0]
	require.Equal(t, "accepted", result["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	var page struct{ Data types.StoryPage }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories", nil, 200), &page))
	for _, story := range page.Data.Items {
		var detail struct {
			Data struct{ Facts []candidateFactWire }
		}
		require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+story.ID, nil, 200), &detail))
		for _, stored := range detail.Data.Facts {
			for _, source := range stored.Sources {
				if source.EntryID == result["entry_id"] {
					return stored
				}
			}
		}
	}
	t.Fatal("processed material has no fact")
	return candidateFactWire{}
}

func candidateFact(subject, object string) map[string]any {
	return map[string]any{
		"title":   "合作",
		"subject": subject,
		"action":  "合作",
		"object":  object,
		"quote":   "A collaboration was announced.",
	}
}

func TestFactEntityLinkingHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	aurora := f.entity(t, "Aurora", "极光")
	partner := f.entity(t, "Partner", "Shared")
	f.entity(t, "Another", "Shared")
	for _, tc := range []struct{ name, subject, object, subjectID, objectID, wantSubject, wantObject string }{
		{"model IDs", "full sentence about Aurora", "unrelated text", aurora.ID, partner.ID, aurora.ID, partner.ID},
		{"normalization", " ＡＵＲＯＲＡ\u3000", "极 光", "", "", aurora.ID, aurora.ID},
		{"invalid IDs", "Unknown institution", "Unknown person", "missing", "missing", "", ""},
		{"exact matching only", "Aurora announced a release", "Shared", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fact := candidateFact(tc.subject, tc.object)
			fact["subject_entity_id"], fact["object_entity_id"] = tc.subjectID, tc.objectID
			stored := f.process(t, tc.name, fact)
			if tc.wantSubject == "" {
				require.Nil(t, stored.SubjectEntityID)
			} else {
				require.NotNil(t, stored.SubjectEntityID)
				require.Equal(t, tc.wantSubject, *stored.SubjectEntityID)
			}
			if tc.wantObject == "" {
				require.Nil(t, stored.ObjectEntityID)
			} else {
				require.NotNil(t, stored.ObjectEntityID)
				require.Equal(t, tc.wantObject, *stored.ObjectEntityID)
			}
		})
	}
}
