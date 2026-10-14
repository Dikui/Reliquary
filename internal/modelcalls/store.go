package modelcalls

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

type Store struct{ DB *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{DB: db} }
func (s *Store) Write(ctx context.Context, call *types.ModelCall) error {
	return s.DB.WithContext(ctx).Create(call).Error
}

type Filter struct {
	SpaceID          uint64
	From, To         *time.Time
	ModelID, EntryID string
	Purpose          types.ModelCallPurpose
}

func (f Filter) Validate() error {
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		return fmt.Errorf("from must precede to")
	}
	if f.Purpose != "" {
		valid := false
		for _, p := range types.ModelCallPurposes {
			if p == f.Purpose {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("unknown purpose")
		}
	}
	return nil
}
func (s *Store) query(ctx context.Context, f Filter) *gorm.DB {
	q := s.DB.WithContext(ctx).Model(&types.ModelCall{}).Where("space_id=?", f.SpaceID)
	if f.From != nil {
		q = q.Where("requested_at>=?", *f.From)
	}
	if f.To != nil {
		q = q.Where("requested_at<?", *f.To)
	}
	if f.ModelID != "" {
		q = q.Where("model_id=?", f.ModelID)
	}
	if f.EntryID != "" {
		q = q.Where("entry_id=?", f.EntryID)
	}
	if f.Purpose != "" {
		q = q.Where("purpose=?", f.Purpose)
	}
	return q
}

type Summary struct {
	ModelID                string                 `json:"model_id"`
	Purpose                types.ModelCallPurpose `json:"purpose"`
	Period                 time.Time              `json:"period"`
	Currency               string                 `json:"currency"`
	Calls                  int64                  `json:"calls"`
	Failures               int64                  `json:"failures"`
	FailureRate            float64                `json:"failure_rate"`
	AverageDurationMS      float64                `json:"average_duration_ms"`
	InputTokens            *int64                 `json:"input_tokens"`
	OutputTokens           *int64                 `json:"output_tokens"`
	UnknownInputCalls      int64                  `json:"unknown_input_calls"`
	UnknownOutputCalls     int64                  `json:"unknown_output_calls"`
	Cost                   *float64               `json:"cost"`
	UnknownCostCalls       int64                  `json:"unknown_cost_calls"`
	NotApplicableCostCalls int64                  `json:"not_applicable_cost_calls"`
}

// Totals cover only known values; unknown counts make partial totals explicit.
const aggregateFields = `currency, COUNT(*) AS calls, COUNT(*) FILTER (WHERE NOT success) AS failures,
 COUNT(*) FILTER (WHERE NOT success)::float8/COUNT(*) AS failure_rate, AVG(duration_ms) AS average_duration_ms,
 SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens,
 COUNT(*) FILTER (WHERE input_tokens IS NULL) AS unknown_input_calls,
 COUNT(*) FILTER (WHERE output_tokens IS NULL) AS unknown_output_calls,
 SUM(cost) AS cost, COUNT(*) FILTER (WHERE cost_status='unknown') AS unknown_cost_calls,
 COUNT(*) FILTER (WHERE cost_status='not_applicable') AS not_applicable_cost_calls`

func (s *Store) Summary(ctx context.Context, f Filter, period string, groups []string) ([]Summary, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if period != "day" && period != "week" && period != "month" {
		return nil, fmt.Errorf("period must be day, week or month")
	}
	selectFields := []string{aggregateFields, fmt.Sprintf("date_trunc('%s',requested_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS period", period)}
	groupFields := []string{"currency", "period"}
	seen := map[string]bool{}
	for _, g := range groups {
		if g != "model" && g != "purpose" {
			return nil, fmt.Errorf("group_by must contain model or purpose")
		}
		if seen[g] {
			continue
		}
		seen[g] = true
		column := g
		if g == "model" {
			column = "model_id"
		}
		selectFields = append(selectFields, column)
		groupFields = append(groupFields, column)
	}
	result := []Summary{}
	err := s.query(ctx, f).Select(strings.Join(selectFields, ",")).Group(strings.Join(groupFields, ",")).Order(strings.Join(groupFields, ",")).Scan(&result).Error
	for i := range result {
		result[i].Period = result[i].Period.UTC()
	}
	return result, err
}
func (s *Store) List(ctx context.Context, f Filter, limit, offset int) ([]types.ModelCall, int64, error) {
	if err := f.Validate(); err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 1000 || offset < 0 {
		return nil, 0, fmt.Errorf("limit must be 1 to 1000 and offset nonnegative")
	}
	var total int64
	if err := s.query(ctx, f).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []types.ModelCall{}
	err := s.query(ctx, f).Order("requested_at DESC,id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	for i := range rows {
		rows[i].RequestedAt = rows[i].RequestedAt.UTC()
	}
	return rows, total, err
}

// Retention defaults to no cleanup. Opt-in days are applied by housekeeping.
func (s *Store) Prune(ctx context.Context) error {
	raw := os.Getenv("RELIQUARY_MODEL_CALL_RETENTION_DAYS")
	if raw == "" || raw == "0" {
		return nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 || days > 36500 {
		return fmt.Errorf("invalid RELIQUARY_MODEL_CALL_RETENTION_DAYS")
	}
	return s.DB.WithContext(ctx).Exec("DELETE FROM model_calls WHERE requested_at < ?", time.Now().UTC().AddDate(0, 0, -days)).Error
}
