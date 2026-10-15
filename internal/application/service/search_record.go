package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type SearchRecorder struct{ db *gorm.DB }

func NewSearchRecorder(db *gorm.DB) *SearchRecorder { return &SearchRecorder{db: db} }

// RecordSearch is shared by ordinary search and the graph service adapter.
// The caller supplies graph parameters as Request and ranked document IDs as
// Results. Scope, origin and evaluation identity are attached here.
func (s *SearchRecorder) RecordSearch(ctx context.Context, record *types.SearchRecord) error {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return apperrors.NewUnauthorizedError("workspace required")
	}
	record.ID = uuid.NewString()
	record.SpaceID = spaceID
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.Source = types.SearchSource(ctx)
	record.EvaluationRunID = types.EvaluationRunID(ctx)
	if record.Method != "ordinary" && record.Method != "graph" {
		return apperrors.NewBadRequestError("invalid search method")
	}
	if record.Request == nil {
		record.Request = json.RawMessage(`{}`)
	}
	if record.Libraries == nil {
		record.Libraries = []string{}
	}
	if record.Results == nil {
		record.Results = []types.RankedEntry{}
	}
	return s.db.WithContext(ctx).Create(record).Error
}

type SearchRecordFilter struct {
	Source            string
	Method            string
	Library           string
	Query             string
	RunID             string
	From              *time.Time
	To                *time.Time
	Limit             int
	Offset            int
	IncludeEvaluation bool
}

func (s *SearchRecorder) List(ctx context.Context, f SearchRecordFilter) ([]types.SearchRecord, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace required")
	}
	q := s.db.WithContext(ctx).Where("space_id=?", spaceID)
	if f.Source != "" {
		q = q.Where("source=?", f.Source)
	} else if !f.IncludeEvaluation && f.RunID == "" {
		q = q.Where("source<>'evaluation'")
	}
	if f.Method != "" {
		q = q.Where("method=?", f.Method)
	}
	if f.Library != "" {
		b, _ := json.Marshal([]string{f.Library})
		q = q.Where("libraries @> ?::jsonb", string(b))
	}
	if f.Query != "" {
		q = q.Where("query ILIKE ?", "%"+f.Query+"%")
	}
	if f.RunID != "" {
		q = q.Where("evaluation_run_id=?", f.RunID)
	}
	if f.From != nil {
		q = q.Where("created_at>=?", *f.From)
	}
	if f.To != nil {
		q = q.Where("created_at<=?", *f.To)
	}
	records := []types.SearchRecord{}
	err := q.Order("created_at DESC,id").Limit(pageLimit(f.Limit)).Offset(max(0, f.Offset)).Find(&records).Error
	return records, err
}
func pageLimit(n int) int {
	if n <= 0 {
		return 50
	}
	return min(n, 200)
}
func SearchRecordRetention() (time.Duration, error) {
	days := 180
	if raw := os.Getenv("RELIQUARY_SEARCH_RECORD_RETENTION_DAYS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 36500 {
			return 0, fmt.Errorf("RELIQUARY_SEARCH_RECORD_RETENTION_DAYS must be 1 to 36500")
		}
		days = n
	}
	return time.Duration(days) * 24 * time.Hour, nil
}
func (s *SearchRecorder) Cleanup(ctx context.Context, now time.Time, retention time.Duration) (int64, error) {
	if retention <= 0 {
		return 0, fmt.Errorf("positive retention required")
	}
	result := s.db.WithContext(ctx).Where("created_at < ?", now.Add(-retention)).Delete(&types.SearchRecord{})
	return result.RowsAffected, result.Error
}

func recordOrdinarySearch(ctx context.Context, db *gorm.DB, request types.EntrySearchRequest, response *types.EntrySearchResponse, started time.Time, searchErr error) error {
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	record := &types.SearchRecord{Method: "ordinary", Query: request.Query, Libraries: request.Libraries, Request: raw, DurationMS: time.Since(started).Milliseconds()}
	if searchErr != nil {
		record.Error = searchErr.Error()
	}
	if response != nil {
		record.FallbackFulltext = response.VectorUnavailable
		record.Reranked = response.RerankStatus == "applied"
		for i, hit := range response.Results {
			if hit != nil {
				record.Results = append(record.Results, types.RankedEntry{EntryID: hit.EntryID, Rank: i + 1, Score: hit.Score})
			}
		}
	}
	// Client cancellation must not erase a completed or failed search record.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return NewSearchRecorder(db).RecordSearch(recordCtx, record)
}
