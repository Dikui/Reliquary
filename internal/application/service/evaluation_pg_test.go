package service

import (
	"context"
	"encoding/json"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

type evaluationModels struct {
	interfaces.ModelService
	llm      chat.Chat
	selected []string
}

func (m *evaluationModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return m.llm, nil
}

func (m *evaluationModels) GetEmbeddingModel(_ context.Context, id string) (embedding.Embedder, error) {
	m.selected = append(m.selected, id)
	return evaluationEmbedder{id: id}, nil
}

type evaluationEmbedder struct {
	embedding.Embedder
	id string
}

func (m evaluationEmbedder) Embed(context.Context, string) ([]float32, error) {
	if m.id == "new-model" {
		return []float32{0, 1, 0}, nil
	}
	return []float32{1, 0, 0}, nil
}

type (
	evaluationChatInterface interface{ chat.Chat }
	evaluationChat          struct {
		evaluationChatInterface
		content string
		onCall  func(context.Context)
	}
)

func (m evaluationChat) Chat(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.onCall != nil {
		m.onCall(ctx)
	}
	return &types.ChatResponse{Content: m.content}, nil
}

func TestEvaluationLifecycleAndRecordCleanup(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "answer-a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	h.add(t, "answer-b", "article", "2026-10-01", "event", "semanticlaunch", 1)
	request := types.EntrySearchRequest{Query: "semanticlaunch", Libraries: []string{h.library.ID}, Limit: 2}
	recorder := NewSearchRecorder(h.db)
	for _, source := range []string{"web", "http", "mcp"} {
		_, err := h.svc.Search(types.WithSearchSource(h.ctx, source), request)
		require.NoError(t, err)
	}
	records, err := recorder.List(h.ctx, SearchRecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 3)
	sources := map[string]bool{}
	for _, r := range records {
		sources[r.Source] = true
		require.Len(t, r.Results, 2)
		require.Equal(t, 1, r.Results[0].Rank)
		require.Equal(t, request.Query, r.Query)
		require.Empty(t, r.EvaluationRunID)
	}
	require.Len(t, sources, 3)
	svc := NewEvaluationService(h.db, h.svc, nil)
	set, err := svc.SaveSet(h.ctx, "", "Retrieval tests", "Hand checked")
	require.NoError(t, err)
	converted, err := svc.QuestionFromRecord(h.ctx, set.ID, records[0].ID, []types.EvaluationAnswer{{EntryID: "answer-a", Grade: 1}, {EntryID: "answer-b", Grade: 2}})
	require.NoError(t, err)
	require.Equal(t, "search_record", converted.Source)
	require.Len(t, converted.SuggestedResults, 2)
	require.Equal(t, request.Query, converted.Request.Query)
	run, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 2})
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)
	require.Len(t, run.Results, 1)
	require.InDelta(t, 1, run.Summary.Recall, 1e-12)
	require.NotContains(t, string(run.Results[0].Snapshot), "folded_member_rule")
	require.Contains(t, string(run.Results[0].Snapshot), "chunking")
	require.Contains(t, string(run.Results[0].Snapshot), "vector_weight")
	records, err = recorder.List(h.ctx, SearchRecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 3)
	evaluations, err := recorder.List(h.ctx, SearchRecordFilter{RunID: run.ID})
	require.NoError(t, err)
	require.Len(t, evaluations, 1)
	require.Equal(t, "evaluation", evaluations[0].Source)
	next, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 1})
	require.NoError(t, err)
	comparison, err := svc.Compare(h.ctx, run.ID, next.ID)
	require.NoError(t, err)
	require.Len(t, comparison.Questions, 1)
	require.Len(t, comparison.Questions[0].LostHits, 1)
	require.InDelta(t, -0.5, comparison.Delta.Recall, 1e-12)
	require.False(t, comparison.Questions[0].QuestionChanged)
	// Historical question and result remain intact after edits and set deletion.
	_, err = svc.SaveQuestion(h.ctx, set.ID, converted.ID, types.EvaluationQuestion{Request: request, Answers: []types.EvaluationAnswer{{EntryID: "answer-b", Grade: 1}}})
	require.NoError(t, err)
	old, err := svc.GetRun(h.ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, old.Results[0].Question.Answers, 2)
	require.NoError(t, h.db.Exec("UPDATE entries SET deleted_at=now() WHERE id='answer-b'").Error)
	question, err := svc.GetQuestion(h.ctx, set.ID, converted.ID)
	require.NoError(t, err)
	require.True(t, question.AnswerDeleted)
	require.Len(t, question.Answers, 1)
	_, err = svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{})
	require.ErrorContains(t, err, "answer document deleted")
	require.NoError(t, h.db.Exec("UPDATE search_records SET created_at=? WHERE id=?", time.Now().AddDate(0, 0, -181), records[0].ID).Error)
	retention, err := SearchRecordRetention()
	require.NoError(t, err)
	require.Equal(t, 180*24*time.Hour, retention)
	removed, err := recorder.Cleanup(h.ctx, time.Now(), retention)
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)
	t.Setenv("RELIQUARY_SEARCH_RECORD_RETENTION_DAYS", "30")
	retention, err = SearchRecordRetention()
	require.NoError(t, err)
	require.Equal(t, 30*24*time.Hour, retention)
	require.NoError(t, svc.DeleteSet(h.ctx, set.ID))
	_, err = svc.GetRun(h.ctx, run.ID)
	require.NoError(t, err)
}

func TestEvaluationAICandidateConfirmationAndPhysicalDeletion(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "answer", "article", "2026-10-01", "event", "semanticlaunch", 1)
	calls := 0
	models := &evaluationModels{llm: evaluationChat{content: `[{"query":"What happened?","answers":[{"entry_id":"answer","grade":1}]}]`, onCall: func(ctx context.Context) {
		calls++
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		require.Equal(t, string(types.CallPurposeEvaluation), stage)
		require.Equal(t, types.ModelTierGeneration, types.ModelTierFromContext(ctx))
		spaceID, _ := types.SpaceIDFromContext(ctx)
		require.EqualValues(t, 1, spaceID)
	}}}
	svc := NewEvaluationService(h.db, h.svc, models)
	set, err := svc.SaveSet(h.ctx, "", "AI candidates", "")
	require.NoError(t, err)
	input := EvaluationGenerateRequest{Libraries: []string{h.library.ID}, EntryIDs: []string{"answer"}, Count: 1, ModelID: "fake"}
	candidates, err := svc.GenerateCandidates(h.ctx, set.ID, input)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, 1, calls)
	require.Equal(t, "pending", candidates[0].Status)
	_, err = svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{})
	require.ErrorContains(t, err, "no confirmed questions")
	_, err = svc.SaveQuestion(h.ctx, set.ID, candidates[0].ID, types.EvaluationQuestion{Request: candidates[0].Request, Answers: candidates[0].Answers, Status: "confirmed"})
	require.NoError(t, err)
	q, err := svc.GetQuestion(h.ctx, set.ID, candidates[0].ID)
	require.NoError(t, err)
	require.Equal(t, "pending", q.Status)
	confirmed, err := svc.ConfirmCandidate(h.ctx, set.ID, q.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "confirmed", confirmed.Status)
	_, err = svc.ConfirmCandidate(h.ctx, set.ID, q.ID, nil)
	require.Error(t, err)
	models.llm = evaluationChat{content: `[{"query":"Bad scope?","answers":[{"entry_id":"other","grade":1}]}]`}
	_, err = svc.GenerateCandidates(h.ctx, set.ID, input)
	require.ErrorContains(t, err, "outside supplied")
	require.NoError(t, h.db.Exec("DELETE FROM entries WHERE id='answer'").Error)
	q, err = svc.GetQuestion(h.ctx, set.ID, q.ID)
	require.NoError(t, err)
	require.True(t, q.AnswerDeleted)
	require.Equal(t, "answer", q.Answers[0].EntryID)
}

func TestEvaluationSelectsActiveCandidateAndArchiveVectors(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.library.IndexingStrategy.KeywordEnabled = false
	require.NoError(t, h.db.Save(h.library).Error)
	h.add(t, "a", "report", "2026-10-01", "event", "semanticlaunch", 1)
	h.add(t, "b", "report", "2026-10-01", "event", "othersemantic", 1)
	for _, id := range []string{"fixture-model", "new-model"} {
		model := types.Model{ID: id, SpaceID: 1, Name: id, Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive}
		model.Parameters.EmbeddingParameters.Dimension = 3
		require.NoError(t, h.db.Create(&model).Error)
	}
	var newModel types.Model
	require.NoError(t, h.db.First(&newModel, "id='new-model'").Error)
	run := types.EmbeddingRebuildRun{ID: "candidate", SpaceID: 1, Candidate: true, Status: "completed", ModelID: "new-model", Dimension: 3, TotalCount: 2, CompletedCount: 2, ProfileHash: types.EmbeddingProfileHash("new-model", 3, &newModel)}
	require.NoError(t, h.db.Create(&run).Error)
	for _, id := range []string{"a", "b"} {
		vec := []float32{0, 0, 1}
		if id == "b" {
			vec = []float32{0, 1, 0}
		}
		require.NoError(t, h.db.Create(&candidateVector{RunID: run.ID, Kind: "report_chunk", SourceID: id + "-chunk-000", EntryID: id, LibraryID: h.library.ID, ModelID: "new-model", Dimension: 3, Embedding: pgvector.NewHalfVector(vec), ContentHash: "fixture"}).Error)
	}
	models := &evaluationModels{}
	search := NewSearchService(entrySearchFixtureBases{library: h.library}, nil, models, h.db)
	svc := NewEvaluationService(h.db, search, models)
	available, err := svc.ListVectorSets(h.ctx)
	require.NoError(t, err)
	require.Len(t, available, 2)
	require.Equal(t, "active", available[0].Selector)
	require.Equal(t, "candidate:candidate", available[1].Selector)
	require.Equal(t, "new-model", available[1].ModelID)
	_, err = svc.ListVectorSets(context.Background())
	require.Error(t, err)
	set, err := svc.SaveSet(h.ctx, "", "Vectors", "")
	require.NoError(t, err)
	_, err = svc.SaveQuestion(
		h.ctx,
		set.ID,
		"",
		types.EvaluationQuestion{
			Request: types.EntrySearchRequest{Libraries: []string{h.library.ID}, Query: "semanticlaunch"},
			Answers: []types.EvaluationAnswer{{EntryID: "b", Grade: 1}},
		},
	)
	require.NoError(t, err)
	old, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 1, VectorSet: "active"})
	require.NoError(t, err)
	var activeSnapshot map[string]any
	require.NoError(t, json.Unmarshal(old.Snapshot, &activeSnapshot))
	require.EqualValues(t, 3, activeSnapshot["vector_set"].(map[string]any)["dimension"])
	require.Equal(t, "a", old.Results[0].Results.Results[0].EntryID)
	fresh, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 1, VectorSet: "candidate:candidate"})
	require.NoError(t, err)
	require.Equal(t, "completed", fresh.Status, fresh.Error)
	require.Equal(t, "b", fresh.Results[0].Results.Results[0].EntryID)
	require.Equal(t, []string{"new-model"}, models.selected)
	comparison, err := svc.Compare(h.ctx, old.ID, fresh.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, comparison.Questions[0].AddedHits)
	require.InDelta(t, 1, comparison.Delta.Recall, 1e-12)
	var candidateSnapshot map[string]any
	require.NoError(t, json.Unmarshal(fresh.Results[0].Snapshot, &candidateSnapshot))
	require.Equal(t, "candidate:candidate", candidateSnapshot["vector_set"].(map[string]any)["selector"])
	require.NoError(t, h.db.Exec("UPDATE embeddings SET model_id='fixture-model'").Error)
	require.NoError(t, h.db.Exec(`INSERT INTO embedding_vector_archives(run_id,space_id,payload) SELECT 'candidate',1,jsonb_build_object('embeddings',(SELECT jsonb_agg(to_jsonb(e)) FROM embeddings e),'article_embeddings','[]'::jsonb,'image_embeddings','[]'::jsonb)`).Error)
	require.NoError(t, h.db.Model(&run).Update("switched_at", time.Now()).Error)
	available, err = svc.ListVectorSets(h.ctx)
	require.NoError(t, err)
	require.Len(t, available, 2)
	require.Equal(t, "archive:candidate", available[1].Selector)
	require.Equal(t, "fixture-model", available[1].ModelID)
	archived, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 1, VectorSet: "archive:candidate"})
	require.NoError(t, err)
	require.Equal(t, "completed", archived.Status, archived.Error)
	require.Equal(t, "a", archived.Results[0].Results.Results[0].EntryID)
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(archived.Results[0].Snapshot, &snapshot))
	require.Equal(t, "archive:candidate", snapshot["vector_set"].(map[string]any)["selector"])
	_, err = svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{VectorSet: "candidate:candidate"})
	require.Error(t, err)
	require.NoError(t, h.db.Exec("DELETE FROM embedding_vector_archives WHERE run_id='candidate'").Error)
	available, err = svc.ListVectorSets(h.ctx)
	require.NoError(t, err)
	require.Len(t, available, 1)
}

func TestEvaluationPreservesReportFactBoundaryAndRecords(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "report-answer", "report", "2026-10-01", "", "semanticlaunch", 1)
	request := types.EntrySearchRequest{Query: "semanticlaunch", Libraries: []string{h.library.ID}, Limit: 1}
	response, err := h.svc.Search(types.WithSearchSource(h.ctx, "web"), request)
	require.NoError(t, err)
	require.Len(t, response.Results, 1)
	require.True(t, response.Results[0].CannotBeFact)
	recorder := NewSearchRecorder(h.db)
	records, err := recorder.List(h.ctx, SearchRecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, response.Results[0].EntryID, records[0].Results[0].EntryID)
	svc := NewEvaluationService(h.db, h.svc, nil)
	set, err := svc.SaveSet(h.ctx, "", "Report boundary", "Integration regression")
	require.NoError(t, err)
	_, err = svc.QuestionFromRecord(h.ctx, set.ID, records[0].ID, []types.EvaluationAnswer{{EntryID: "report-answer", Grade: 1}})
	require.NoError(t, err)
	run, err := svc.Run(h.ctx, set.ID, types.EvaluationRunOptions{K: 1})
	require.NoError(t, err)
	saved, err := svc.GetRun(h.ctx, run.ID)
	require.NoError(t, err)
	require.True(t, saved.Results[0].Results.Results[0].CannotBeFact)
	require.Equal(t, "report-answer", saved.Results[0].Results.Results[0].EntryID)
	require.InDelta(t, 1, saved.Summary.Recall, 1e-12)
	daily, err := recorder.List(h.ctx, SearchRecordFilter{})
	require.NoError(t, err)
	require.Len(t, daily, 1)
	evaluation, err := recorder.List(h.ctx, SearchRecordFilter{RunID: run.ID})
	require.NoError(t, err)
	require.Len(t, evaluation, 1)
	require.Equal(t, "evaluation", evaluation[0].Source)
	require.Equal(t, "report-answer", evaluation[0].Results[0].EntryID)
}
