package service

import (
	"context"
	"encoding/json"
	"errors"
	"reliquary/internal/models/chat"
	"reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type checkTestModels struct {
	interfaces.ModelService
	model chat.Chat
}

func (m checkTestModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return m.model, nil
}

type checkTestChat struct {
	replies []string
	failAt  int
	calls   int
}

func (m *checkTestChat) GetModelName() string { return "fixture" }
func (m *checkTestChat) GetModelID() string   { return "local-fixture" }
func (m *checkTestChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("unused")
}

func (m *checkTestChat) Chat(_ context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.calls++
	if m.calls == m.failAt {
		return nil, errors.New("429 rate limit reached")
	}
	return &types.ChatResponse{Content: m.replies[(m.calls-1)%len(m.replies)]}, nil
}

type checkTestSearch struct {
	interfaces.SearchService
	t   *testing.T
	ids []string
}

func (s checkTestSearch) Search(_ context.Context, r types.EntrySearchRequest) (*types.EntrySearchResponse, error) {
	require.Empty(s.t, r.PublishedFrom)
	require.Empty(s.t, r.PublishedTo)
	require.NotNil(s.t, r.CollapseSimilar)
	require.False(s.t, *r.CollapseSimilar)
	out := &types.EntrySearchResponse{}
	for _, id := range s.ids {
		out.Results = append(out.Results, &types.EntrySearchResult{EntryID: id})
	}
	return out, nil
}

func seedCheckEntry(t *testing.T, h *articleProcessingHarness, libraryID, title, body string) *types.Entry {
	t.Helper()
	e := &types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: libraryID, Title: title, VaultBody: body, VaultPath: uuid.NewString() + ".md", SourceKind: "article", ParseStatus: types.ParseStatusCompleted, SummaryStatus: types.SummaryStatusCompleted, EnableStatus: "enabled", ContentType: "event"}
	require.NoError(t, h.db.Create(e).Error)
	return e
}

func checkTask(id string) *queue.Task {
	b, _ := json.Marshal(map[string]string{"run_id": id})
	return queue.NewTask(types.TypeContentCheck, b)
}

func TestContentCheckReportsEvidenceThroughRunResult(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.service.vault = &VaultService{db: h.db}
	old := seedCheckEntry(t, h, h.library.ID, "History", "Acme specification.")
	otherLibrary := &types.Library{ID: uuid.NewString(), SpaceID: 1, Name: "Other sources", Directory: "other-sources"}
	require.NoError(t, h.db.Create(otherLibrary).Error)
	conflict := seedCheckEntry(t, h, otherLibrary.ID, "Earlier specification", "Acme supports eight connections.")
	require.NoError(t, h.db.Model(conflict).Updates(map[string]any{"metadata": types.JSON(`{"source":{"published_at":"2025-01-01T00:00:00Z"}}`)}).Error)
	model := &checkTestChat{}
	s := NewContentCheckService(
		h.db,
		&libraryRebuildQueue{db: h.db},
		checkTestModels{model: model},
		h.service,
		checkTestSearch{t: t, ids: []string{old.ID, conflict.ID}},
		NewEntityService(h.db, h.service.modelService),
	)
	require.NoError(
		t,
		s.Configure(
			h.ctx,
			types.ContentCheckSettings{Enabled: true, ModelID: "local-fixture", Weekday: 1, Time: "03:00"},
		),
	)
	target := seedCheckEntry(t, h, h.library.ID, "New source", "Acme supports sixteen connections. Acme specification.")
	model.replies = []string{`{"tool":"search","arguments":{"query":"Acme connections"}}`, `{"tool":"get_entry","arguments":{"id":"` + old.ID + `"}}`, `{"tool":"get_entry","arguments":{"id":"` + conflict.ID + `"}}`, `{"findings":[{"kind":"conflict","description":"The same object's connection count differs.","evidence":[{"entry_id":"` + target.ID + `","quote":"Acme supports sixteen connections."},{"entry_id":"` + conflict.ID + `","quote":"Acme supports eight connections."}]},{"kind":"duplicate","description":"Same specification content.","evidence":[{"entry_id":"` + target.ID + `","quote":"Acme specification."},{"entry_id":"` + old.ID + `","quote":"Acme specification."}]},{"kind":"result_error","description":"Existing result does not match the source.","evidence":[{"entry_id":"` + target.ID + `","quote":"Acme supports sixteen connections.","current_result":"eight connections"}]}]}`}
	target.StructuredSummary = &types.EntrySummary{Overview: "eight connections"}
	require.NoError(t, h.db.Save(target).Error)
	before := target.VaultBody
	run, err := s.Start(h.ctx, "")
	require.NoError(t, err)
	require.Len(t, run.Targets, 1)
	require.NoError(t, s.Handle(h.ctx, checkTask(run.ID)))
	got, err := s.Get(h.ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", got.Status, got.Error)
	require.Equal(t, 1, got.Completed)
	require.Len(t, got.Results, 1)
	require.Len(t, got.Results[0].Findings, 3)
	ev := got.Results[0].Findings[0].Evidence[0]
	require.Equal(t, target.ID, ev.EntryID)
	require.Equal(t, "exact", ev.Location.Mapping)
	require.NotEmpty(t, ev.Location.BodyHash)
	require.Equal(t, otherLibrary.ID, got.Results[0].Findings[0].Evidence[1].LibraryID)
	require.Equal(t, target.LibraryID, got.Results[0].Findings[1].Evidence[1].LibraryID)
	settings, err := s.Settings(h.ctx)
	require.NoError(t, err)
	require.NotNil(t, settings.NextRunAt)
	require.Equal(t, time.Monday, settings.NextRunAt.In(time.FixedZone("Asia/Shanghai", 8*3600)).Weekday())
	var after types.Entry
	require.NoError(t, h.db.First(&after, "id=?", target.ID).Error)
	require.Equal(t, before, after.VaultBody)
	page, err := s.List(h.ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
}

func TestContentCheckStopsAndRetainsResultsForExplicitRecheck(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.service.vault = &VaultService{db: h.db}
	model := &checkTestChat{
		replies: []string{`{"tool":"search","arguments":{"query":"Acme"}}`, `{"findings":[]}`},
		failAt:  3,
	}
	s := NewContentCheckService(
		h.db,
		&libraryRebuildQueue{db: h.db},
		checkTestModels{model: model},
		h.service,
		checkTestSearch{t: t},
		NewEntityService(h.db, h.service.modelService),
	)
	require.NoError(
		t,
		s.Configure(
			h.ctx,
			types.ContentCheckSettings{Enabled: true, ModelID: "local-fixture", Weekday: 1, Time: "03:00"},
		),
	)
	first := seedCheckEntry(t, h, h.library.ID, "First", "First Acme note.")
	time.Sleep(time.Millisecond)
	second := seedCheckEntry(t, h, h.library.ID, "Second", "Second Acme note.")
	run, err := s.Start(h.ctx, "")
	require.NoError(t, err)
	require.NoError(t, s.Handle(h.ctx, checkTask(run.ID)))
	failed, err := s.Get(h.ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, 1, failed.Completed, failed.Error)
	require.Contains(t, failed.Error, "429")
	require.Equal(t, 3, model.calls)
	require.NoError(t, s.Handle(h.ctx, checkTask(run.ID)))
	require.Equal(t, 3, model.calls, "failed work must not retry itself")
	model.calls = 0
	model.failAt = 0
	retry, err := s.Start(h.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, retry.Targets, 1)
	require.Equal(t, second.ID, retry.Targets[0])
	require.NotEqual(t, first.ID, retry.Targets[0])
	require.NoError(t, s.Handle(h.ctx, checkTask(retry.ID)))
	completed, err := s.Get(h.ctx, retry.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Status)
	original, err := s.Get(h.ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", original.Status)
	require.Len(t, original.Results, 1)
	seedCheckEntry(t, h, h.library.ID, "Queue interruption", "Another Acme note.")
	interrupted, err := s.Start(h.ctx, "")
	require.NoError(t, err)
	require.NoError(t, s.FailTask(h.ctx, checkTask(interrupted.ID), errors.New("task handler panic")))
	terminal, err := s.Get(h.ctx, interrupted.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", terminal.Status)
	require.NoError(t, s.FailTask(h.ctx, checkTask(completed.ID), errors.New("late callback")))
	completed, err = s.Get(h.ctx, completed.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", completed.Status)
}
