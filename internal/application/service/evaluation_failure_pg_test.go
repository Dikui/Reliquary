package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"
)

type evaluationBrokenSearch struct{ interfaces.SearchService }

func (evaluationBrokenSearch) Search(context.Context, types.EntrySearchRequest) (*types.EntrySearchResponse, error) {
	return nil, fmt.Errorf("fixture retrieval outage")
}

func TestEvaluationFoldedMembersNeverCountAndFailurePersists(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	h.add(t, "b", "article", "2026-10-01", "event", "semanticlaunch", 1)
	svc := NewEvaluationService(h.db, h.svc, nil)
	set, err := svc.SaveSet(h.ctx, "", "Folded answer", "")
	require.NoError(t, err)
	_, err = svc.SaveQuestion(h.ctx, set.ID, "", types.EvaluationQuestion{Request: types.EntrySearchRequest{Libraries: []string{h.library.ID}, Query: "semanticlaunch"}, Answers: []types.EvaluationAnswer{{EntryID: "b", Grade: 1}}})
	require.NoError(t, err)
	folded, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 2})
	require.NoError(t, err)
	require.Equal(t, "completed", folded.Status, folded.Error)
	require.Len(t, folded.Results[0].Results.Results, 1)
	require.Equal(t, "b", folded.Results[0].Results.Results[0].SimilarReports[0].EntryID)
	require.Zero(t, folded.Summary.Recall)
	unfolded, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 2, CollapseSimilar: entrySearchNoCollapse()})
	require.NoError(t, err)
	require.InDelta(t, 1, unfolded.Summary.Recall, 1e-12)
	failed, err := NewEvaluationService(h.db, evaluationBrokenSearch{}, nil).Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 2})
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.Contains(t, failed.Error, "fixture retrieval outage")
	require.Contains(t, failed.Results[0].Error, "outage")
	require.NotNil(t, failed.FinishedAt)
	_, err = svc.Compare(h.ctx, folded.ID, failed.ID)
	require.ErrorContains(t, err, "completed")
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(unfolded.Snapshot, &snapshot))
	require.EqualValues(t, 2, snapshot["k"])
}

func TestGraphRecordContractAndEvaluationIsolation(t *testing.T) {
	h := newEntrySearchFixture(t)
	recorder := NewSearchRecorder(h.db)
	record := &types.SearchRecord{Method: "graph", Query: "entity-a", Libraries: []string{h.library.ID}, Request: json.RawMessage(`{"libraries":["search-library"],"query":"entity-a","entity_ids":["entity-a"]}`), Results: []types.RankedEntry{{EntryID: "doc", Rank: 1}}, DurationMS: 12}
	require.NoError(t, recorder.RecordSearch(types.WithEvaluationRun(types.WithSearchSource(h.ctx, "mcp"), "graph-run"), record))
	rows, err := recorder.List(h.ctx, SearchRecordFilter{})
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = recorder.List(h.ctx, SearchRecordFilter{Method: "graph", RunID: "graph-run", Library: h.library.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "evaluation", rows[0].Source)
	require.Equal(t, "graph", rows[0].Method)
}
