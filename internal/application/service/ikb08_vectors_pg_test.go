package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/models/embedding"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/sourceloc"
	"reliquary/internal/types"
	"reliquary/internal/utils/ollama"
	"strings"
	"testing"
)

func TestIKB08CandidateVectorsKeepOldUntilAtomicSwitch(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	require.NoError(t, h.db.Model(&types.Space{}).Where("id=1").Update("embedding_model_id", "fixture-model").Error)
	copy := *h.rebuilder
	copy.targetModelID = "replacement-model"
	run, err := copy.Start(h.ctx, 1)
	require.NoError(t, err)
	require.True(t, run.Candidate)
	require.Error(t, h.rebuilder.SwitchCandidate(h.ctx, 1, run.ID))
	checks := 0
	h.embedder.onEmbed = func(context.Context, string) error {
		checks++
		var current string
		require.NoError(t, h.db.Raw("SELECT embedding_model_id FROM spaces WHERE id=1").Scan(&current).Error)
		require.Equal(t, "fixture-model", current)
		var changed int64
		require.NoError(t, h.db.Table("embeddings").Where("model_id='replacement-model'").Count(&changed).Error)
		require.Zero(t, changed)
		response := h.search(t, types.EntrySearchRequest{Query: "semanticlaunch", Libraries: []string{h.library.ID}, CollapseSimilar: entrySearchNoCollapse()})
		require.NotEmpty(t, response.Results)
		return nil
	}
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Positive(t, checks)
	require.NoError(t, h.rebuilder.SwitchCandidate(h.ctx, 1, run.ID))
	var current string
	require.NoError(t, h.db.Raw("SELECT embedding_model_id FROM spaces WHERE id=1").Scan(&current).Error)
	require.Equal(t, "replacement-model", current)
	var old int64
	require.NoError(t, h.db.Table("embeddings").Where("model_id<>'replacement-model'").Count(&old).Error)
	require.Zero(t, old)
	require.NoError(t, h.db.Table("embedding_vector_archives").Where("run_id=?", run.ID).Count(&old).Error)
	require.EqualValues(t, 1, old)
	response := h.search(t, types.EntrySearchRequest{Query: "searchfixture", Libraries: []string{h.library.ID}})
	require.NotEmpty(t, response.Results)
	require.NoError(t, h.rebuilder.DeleteOldVectors(h.ctx, 1, run.ID))
	require.NoError(t, h.db.Table("embedding_vector_archives").Count(&old).Error)
	require.Zero(t, old)
}

func TestIKB08CandidateRejectsChangedBody(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	copy := *h.rebuilder
	copy.targetModelID = "replacement-model"
	run, err := copy.Start(h.ctx, 1)
	require.NoError(t, err)
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("id='full-chunk-000'").Update("content", "new content").Error)
	require.ErrorContains(t, h.rebuilder.SwitchCandidate(h.ctx, 1, run.ID), "sources changed")
}

func TestIKB08EntryVectorPendingRecoversWithoutReprocessing(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	taskData, _ := json.Marshal(map[string]any{"space_id": 1, "entry_id": h.full.ID})
	task := taskqueue.NewTask(types.TypeEntryVector, taskData)
	offline := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/":
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3-embedding:0.6b"}]}`))
		case "/api/embed":
			_, _ = w.Write([]byte(`{"embeddings":[[1,0,0]]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("OLLAMA_BASE_URL", server.URL)
	local, err := ollama.GetOllamaService()
	require.NoError(t, err)
	embedder, err := embedding.NewEmbedder(embedding.Config{Source: types.ModelSourceLocal, ModelName: "qwen3-embedding:0.6b", ModelID: "replacement-model", Dimensions: 3}, nil, local)
	require.NoError(t, err)
	h.rebuilder.profiles.models = ikb08Models{embedder: embedder}
	err = h.rebuilder.HandleEntryVector(h.ctx, task)
	var deferred types.DeferredOperation
	require.ErrorAs(t, err, &deferred)
	offline = false
	require.NoError(t, h.rebuilder.HandleEntryVector(h.ctx, task))

	var count int64
	require.NoError(t, h.db.Table("embeddings").Where("entry_id=? AND model_id=?", h.full.ID, "replacement-model").Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestIKB08SupplementRecalculatesBodyLocations(t *testing.T) {
	h := newEntrySearchFixture(t)
	entry := h.add(t, "supplement", "mine", "", "", "", 0)
	old := "# 标题 😀\n\n前文\n\n![图](https://example.org/a.png)\n\n正文 tail"
	next := "# 标题 😀\n\n前文\n\n![图](../attachments/supplement/a.png)\n\n正文 tail"
	parsed, _ := splitArticleBody(old, types.ChunkingConfig{ChunkSize: 40, ChunkOverlap: 5})
	chunks := []*types.Chunk{}
	for i, p := range parsed {
		chunks = append(chunks, &types.Chunk{ID: entry.ID + string(rune('a'+i)), SpaceID: 1, LibraryID: h.library.ID, EntryID: entry.ID, ChunkType: types.ChunkTypeText, Content: p.Content, StartAt: p.Start, EndAt: p.End})
	}
	sourceloc.Assign(old, chunks)
	require.NoError(t, h.db.Create(chunks).Error)
	require.NoError(t, recalculateBodyLocations(h.ctx, h.db, entry, old, next, map[string]string{"https://example.org/a.png": "../attachments/supplement/a.png"}))
	var after []types.Chunk
	require.NoError(t, h.db.Where("entry_id=?", entry.ID).Find(&after).Error)
	for _, c := range after {
		require.NotEmpty(t, c.SourceLocations)
		require.Equal(t, c.Content, string([]rune(next)[c.StartAt:c.EndAt]))
	}
}

func TestIKB08CancelCandidateFencesQueuedTask(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	copy := *h.rebuilder
	copy.targetModelID = "replacement-model"
	run, err := copy.Start(h.ctx, 1)
	require.NoError(t, err)
	require.NoError(t, h.rebuilder.CancelCandidate(h.ctx, 1, run.ID))
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, 0, h.embedder.calls)
	_, err = copy.Start(h.ctx, 1)
	require.NoError(t, err)
}

func TestIKB08PendingVectorCannotPublishAfterModelSwitch(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	h.embedder.onEmbed = func(context.Context, string) error {
		require.NoError(t, h.db.Model(&types.Space{}).Where("id=1").Update("embedding_model_id", "newly-selected").Error)
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"space_id": 1, "entry_id": h.full.ID})
	err := h.rebuilder.HandleEntryVector(h.ctx, taskqueue.NewTask(types.TypeEntryVector, payload))
	var deferred types.DeferredOperation
	require.ErrorAs(t, err, &deferred)
	var stale int64
	require.NoError(t, h.db.Table("embeddings").Where("entry_id=? AND model_id='replacement-model'", h.full.ID).Count(&stale).Error)
	require.Zero(t, stale)
}

func TestIKB08SupplementHTMLImagePreservesRanges(t *testing.T) {
	h := newEntrySearchFixture(t)
	entry := h.add(t, "html-image", "mine", "", "", "", 0)
	old := "# 中文 😀\n\n<img src=\"https://example.org/a-very-long-image-name.png\" alt=\"图\">\n\n尾部"
	next := "# 中文 😀\n\n<img src=\"../a.png\" alt=\"图\">\n\n尾部"
	c := &types.Chunk{ID: "html-chunk", SpaceID: 1, LibraryID: h.library.ID, EntryID: entry.ID, Content: old, EndAt: len([]rune(old)), ChunkType: types.ChunkTypeText}
	sourceloc.Assign(old, []*types.Chunk{c})
	require.NoError(t, h.db.Create(c).Error)
	require.NoError(t, recalculateBodyLocations(h.ctx, h.db, entry, old, next, nil))
	var after types.Chunk
	require.NoError(t, h.db.First(&after, "id=?", c.ID).Error)
	require.Equal(t, next, after.Content)
	require.Equal(t, len([]rune(next)), after.EndAt)
	require.NotEmpty(t, after.SourceLocations)
	refs := bodyImageReferences(next)
	require.Len(t, refs, 1)
	require.Equal(t, "../a.png", refs[0].URL)
}

func TestIKB08StoredCRLFPositionsUseSavedBodyCoordinates(t *testing.T) {
	h := newEntrySearchFixture(t)
	entry := h.add(t, "crlf-body", "report", "", "", "", 0)
	body := "# 标题 😀\r\n\r\n第一段。\r\n\r\n## Child\r\n\r\nEnglish 中文。"
	require.NoError(t, h.db.Model(entry).Update("vault_body", body).Error)
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	parts, _ := splitArticleBody(normalized, types.ChunkingConfig{ChunkSize: 25, ChunkOverlap: 0, ChunkOverlapSet: true})
	for i, p := range parts {
		c := &types.Chunk{ID: fmt.Sprintf("crlf-%d", i), SpaceID: 1, LibraryID: h.library.ID, EntryID: entry.ID, Content: p.Content, StartAt: p.Start, EndAt: p.End, ChunkType: types.ChunkTypeText}
		require.NoError(t, h.db.Create(c).Error)
		require.NotEmpty(t, c.SourceLocations)
		var selected strings.Builder
		for _, loc := range c.SourceLocations {
			selected.WriteString(string([]rune(body)[loc.Start:loc.End]))
			require.Equal(t, "normalized_line_endings", loc.Mapping)
		}
		require.Equal(t, c.Content, strings.ReplaceAll(selected.String(), "\r\n", "\n"))
	}
}
