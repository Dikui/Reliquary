package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"reliquary/internal/application/service/metric"
	"reliquary/internal/types"
	"slices"
	"strings"
	"time"

	apperrors "reliquary/internal/errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validateRunOptions(options *types.EvaluationRunOptions) error {
	if options.K == 0 {
		options.K = 10
	}
	if options.K < 1 || options.K > 100 {
		return apperrors.NewBadRequestError("k must be 1 to 100")
	}
	if options.VectorSet == "" {
		options.VectorSet = "active"
	}
	if options.RetrievalConfig != nil {
		c := options.RetrievalConfig
		if c.EmbeddingTopK < 0 || c.EmbeddingTopK > 10000 || c.RRFK < 0 || c.RRFK > 10000 || c.VectorThreshold < 0 || c.VectorThreshold > 1 || c.KeywordThreshold < 0 || c.KeywordThreshold > 1 || c.RRFVectorWeight < 0 || c.RRFKeywordWeight < 0 || c.RRFImageWeight < 0 {
			return apperrors.NewBadRequestError("invalid retrieval config")
		}
	}
	return nil
}

// Run executes the same public retrieval service used by HTTP and MCP. The
// initial transaction freezes questions and answers, so edits cannot rewrite
// historical results. Execution is synchronous and obeys request cancellation.
func (s *EvaluationService) Run(ctx context.Context, setID string, options types.EvaluationRunOptions) (*types.EvaluationRun, error) {
	if err := validateRunOptions(&options); err != nil {
		return nil, err
	}
	set, err := s.GetSet(ctx, setID)
	if err != nil {
		return nil, err
	}
	if _, err := resolveSearchVectorSet(ctx, s.db, set.SpaceID, options.VectorSet, false); err != nil {
		return nil, err
	}
	run := &types.EvaluationRun{ID: uuid.NewString(), SpaceID: set.SpaceID, SetID: setID, SetSnapshot: *set, Options: options, Status: "running"}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		questions := []types.EvaluationQuestion{}
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("space_id=? AND set_id=? AND status='confirmed'", set.SpaceID, setID).Preload("Answers", func(db *gorm.DB) *gorm.DB { return db.Order("entry_id") }).Order("created_at,id").Find(&questions).Error; err != nil {
			return err
		}
		if len(questions) == 0 {
			return apperrors.NewBadRequestError("evaluation set has no confirmed questions")
		}
		if len(questions) > 1000 {
			return apperrors.NewBadRequestError("at most 1000 questions per run")
		}
		if err := freezeEvaluationRun(ctx, tx, run, questions); err != nil {
			return err
		}
		options = run.Options
		for _, q := range questions {
			if q.AnswerDeleted {
				return apperrors.NewBadRequestError("answer document deleted for question " + q.ID)
			}
			if q.Method != "ordinary" {
				return apperrors.NewBadRequestError("only ordinary retrieval can be evaluated")
			}
			run.Results = append(run.Results, types.EvaluationRunResult{RunID: run.ID, QuestionID: q.ID, Question: q, Snapshot: json.RawMessage(`{}`), Results: &types.EntrySearchResponse{Results: []*types.EntrySearchResult{}}, Hits: []string{}})
		}
		if err := tx.Omit("Results").Create(run).Error; err != nil {
			return err
		}
		return tx.Create(&run.Results).Error
	})
	if err != nil {
		return nil, err
	}
	executionCtx := types.WithSearchExecution(types.WithEvaluationRun(ctx, run.ID), options)
	failures := []string{}
	for i := range run.Results {
		row := &run.Results[i]
		request := row.Question.Request
		request.Limit = options.K
		request.VectorSet = options.VectorSet
		var response *types.EntrySearchResponse
		var searchErr error
		if err := ctx.Err(); err != nil {
			searchErr = err
		} else {
			response, searchErr = s.search.Search(executionCtx, request)
		}
		if searchErr == nil && response == nil {
			searchErr = fmt.Errorf("retrieval service returned no response")
		}
		if response != nil {
			row.Results = response
			if len(response.Snapshot) > 0 {
				row.Snapshot = response.Snapshot
			}
		}
		if searchErr != nil {
			row.Error = searchErr.Error()
			failures = append(failures, row.QuestionID+": "+row.Error)
		} else {
			ids := []string{}
			for _, result := range response.Results {
				if result != nil {
					ids = append(ids, result.EntryID)
				}
			}
			row.Metrics, row.Hits = metric.Compute(ids, row.Question.Answers, options.K)
		}
		writeCtx, cancel := detachEvaluationContext(ctx)
		err = s.db.WithContext(writeCtx).Save(row).Error
		cancel()
		if err != nil {
			failures = append(failures, "persist result: "+err.Error())
			break
		}
	}
	for _, row := range run.Results {
		run.Summary = addEvaluationMetrics(run.Summary, row.Metrics, 1/float64(len(run.Results)))
	}
	run.Status = "completed"
	if len(failures) > 0 {
		run.Status = "failed"
		run.Error = strings.Join(failures, "; ")
	}
	finished := time.Now().UTC()
	run.FinishedAt = &finished
	writeCtx, cancel := detachEvaluationContext(ctx)
	defer cancel()
	if err := s.db.WithContext(writeCtx).Omit("Results").Save(run).Error; err != nil {
		return nil, err
	}
	return s.GetRun(writeCtx, run.ID)
}

func freezeEvaluationRun(ctx context.Context, tx *gorm.DB, run *types.EvaluationRun, questions []types.EvaluationQuestion) error {
	var space types.Space
	activeDimension := 0
	if err := tx.Where("id=?", run.SpaceID).First(&space).Error; err != nil {
		return err
	}
	if run.Options.RetrievalConfig == nil {
		run.Options.RetrievalConfig = &types.RetrievalConfig{}
		if space.RetrievalConfig != nil {
			*run.Options.RetrievalConfig = *space.RetrievalConfig
		}
	}
	if run.Options.VectorSet == "active" {
		run.Options.ExpectedEmbeddingModelID = space.EmbeddingModelID
		var model types.Model
		result := tx.Where("id=? AND (space_id=? OR is_builtin=true)", space.EmbeddingModelID, run.SpaceID).Find(&model)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			run.Options.ExpectedEmbeddingProfileHash = types.EmbeddingProfileHash(model.ID, model.Parameters.EmbeddingParameters.Dimension, &model)
			activeDimension = model.Parameters.EmbeddingParameters.Dimension
		}
	}
	ids := []string{}
	for _, q := range questions {
		ids = append(ids, q.Request.Libraries...)
	}
	libraries := []types.Library{}
	if err := tx.Where("space_id=? AND id IN ?", run.SpaceID, ids).Order("id").Find(&libraries).Error; err != nil {
		return err
	}
	set, err := resolveSearchVectorSet(ctx, tx, run.SpaceID, run.Options.VectorSet, false)
	if err != nil {
		return err
	}
	if set.Selector == "active" {
		set.ModelID = run.Options.ExpectedEmbeddingModelID
		set.ProfileHash = run.Options.ExpectedEmbeddingProfileHash
		set.Dimension = activeDimension
	}
	librarySnapshots := []map[string]any{}
	for _, lib := range libraries {
		rerankID, err := ResolveModelTier(ctx, tx, &lib, types.ModelTierRerank)
		if err != nil {
			return err
		}
		if run.Options.RerankModelID != nil {
			rerankID = *run.Options.RerankModelID
		}
		librarySnapshots = append(librarySnapshots, map[string]any{"id": lib.ID, "chunking": lib.ChunkingConfig, "indexing": lib.IndexingStrategy, "rerank_model_id": rerankID})
	}
	run.Snapshot, _ = json.Marshal(
		map[string]any{
			"version":          1,
			"libraries":        librarySnapshots,
			"vector_set":       set,
			"retrieval_config": run.Options.RetrievalConfig,
			"k":                run.Options.K,
		},
	)
	return nil
}

func addEvaluationMetrics(a, b types.EvaluationMetrics, factor float64) types.EvaluationMetrics {
	return types.EvaluationMetrics{Precision: a.Precision + b.Precision*factor, Recall: a.Recall + b.Recall*factor, MRR: a.MRR + b.MRR*factor, NDCG: a.NDCG + b.NDCG*factor, MAP: a.MAP + b.MAP*factor}
}

func (s *EvaluationService) GetRun(ctx context.Context, id string) (*types.EvaluationRun, error) {
	scope, err := evaluationScope(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var run types.EvaluationRun
	if err := scope.Where("id=?", id).Preload("Results", func(db *gorm.DB) *gorm.DB { return db.Order("question_id") }).First(&run).Error; err != nil {
		return nil, evaluationError(err)
	}
	return &run, nil
}

func (s *EvaluationService) ListRuns(ctx context.Context, setID string, limit, offset int) ([]types.EvaluationRun, error) {
	scope, err := evaluationScope(ctx, s.db)
	if err != nil {
		return nil, err
	}
	if setID != "" {
		scope = scope.Where("set_id=?", setID)
	}
	runs := []types.EvaluationRun{}
	err = scope.Order("created_at DESC,id").Limit(pageLimit(limit)).Offset(max(0, offset)).Find(&runs).Error
	return runs, err
}

type EvaluationQuestionDelta struct {
	QuestionID      string                     `json:"question_id"`
	Change          string                     `json:"change"`
	QuestionChanged bool                       `json:"question_changed"`
	Before          *types.EvaluationRunResult `json:"before"`
	After           *types.EvaluationRunResult `json:"after"`
	Delta           types.EvaluationMetrics    `json:"delta"`
	AddedHits       []string                   `json:"added_hits"`
	LostHits        []string                   `json:"lost_hits"`
}
type EvaluationComparison struct {
	BeforeID      string                    `json:"before_id"`
	AfterID       string                    `json:"after_id"`
	BeforeSummary types.EvaluationMetrics   `json:"before_summary"`
	AfterSummary  types.EvaluationMetrics   `json:"after_summary"`
	Delta         types.EvaluationMetrics   `json:"delta"`
	Questions     []EvaluationQuestionDelta `json:"questions"`
}

func (s *EvaluationService) Compare(ctx context.Context, beforeID, afterID string) (*EvaluationComparison, error) {
	before, err := s.GetRun(ctx, beforeID)
	if err != nil {
		return nil, err
	}
	after, err := s.GetRun(ctx, afterID)
	if err != nil {
		return nil, err
	}
	if before.SetID != after.SetID {
		return nil, apperrors.NewBadRequestError("runs must belong to the same evaluation set")
	}
	if before.Status != "completed" || after.Status != "completed" {
		return nil, apperrors.NewBadRequestError("only completed runs can be compared")
	}
	comparison := &EvaluationComparison{BeforeID: beforeID, AfterID: afterID, BeforeSummary: before.Summary, AfterSummary: after.Summary, Delta: addEvaluationMetrics(after.Summary, before.Summary, -1), Questions: []EvaluationQuestionDelta{}}
	left := map[string]*types.EvaluationRunResult{}
	right := map[string]*types.EvaluationRunResult{}
	ids := []string{}
	for i := range before.Results {
		row := &before.Results[i]
		left[row.QuestionID] = row
		ids = append(ids, row.QuestionID)
	}
	for i := range after.Results {
		row := &after.Results[i]
		right[row.QuestionID] = row
		if left[row.QuestionID] == nil {
			ids = append(ids, row.QuestionID)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		a, b := left[id], right[id]
		row := EvaluationQuestionDelta{QuestionID: id, Before: a, After: b, Change: "retained", AddedHits: []string{}, LostHits: []string{}}
		am, bm := types.EvaluationMetrics{}, types.EvaluationMetrics{}
		ah, bh := []string{}, []string{}
		if a != nil {
			am = a.Metrics
			ah = a.Hits
		} else {
			row.Change = "added"
		}
		if b != nil {
			bm = b.Metrics
			bh = b.Hits
		} else {
			row.Change = "removed"
		}
		row.Delta = addEvaluationMetrics(bm, am, -1)
		if a != nil && b != nil {
			row.QuestionChanged = !reflect.DeepEqual(a.Question, b.Question)
		}
		for _, hit := range bh {
			if !slices.Contains(ah, hit) {
				row.AddedHits = append(row.AddedHits, hit)
			}
		}
		for _, hit := range ah {
			if !slices.Contains(bh, hit) {
				row.LostHits = append(row.LostHits, hit)
			}
		}
		comparison.Questions = append(comparison.Questions, row)
	}
	return comparison, nil
}
