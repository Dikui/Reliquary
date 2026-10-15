package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

const defaultMaxSegments = 128
const defaultSegmentOverlap = 128
const defaultContextReservePercent = 40

// Reserve the bounded heading path, preceding context and segment envelope.
const segmentEnvelopeReserve = 2048

type segmentCheckpoint struct {
	types.ProcessingSegment
	Status string          `json:"status"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type entrySegmentRun struct {
	ID        string `gorm:"primaryKey"`
	SpaceID   uint64
	EntryID   string
	Phase     string
	PlanHash  string
	BodyHash  string
	Plan      types.JSON `gorm:"type:jsonb"`
	Result    types.JSON `gorm:"type:jsonb"`
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (entrySegmentRun) TableName() string { return "entry_segment_runs" }

func segmentHash(value any) string {
	data, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// An explicit rerun of a finished operation is a new logical run. Ordinary
// task retries keep the generation, including retries after partial failure.
func advanceSegmentGeneration(entry *types.Entry, phase string) error {
	metadata := map[string]json.RawMessage{}
	if len(entry.Metadata) > 0 {
		if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
			return err
		}
	}
	value, _ := json.Marshal(uuid.NewString())
	metadata["segment_"+phase+"_generation"] = value
	data, err := json.Marshal(metadata)
	entry.Metadata = types.JSON(data)
	return err
}

func segmentGeneration(entry *types.Entry, phase string) json.RawMessage {
	metadata := map[string]json.RawMessage{}
	_ = json.Unmarshal(entry.Metadata, &metadata)
	return metadata["segment_"+phase+"_generation"]
}

func (s *entryService) segmentSettings() (limit, overlap, reserve int) {
	limit, overlap, reserve = defaultMaxSegments, defaultSegmentOverlap, defaultContextReservePercent
	if s.config != nil && s.config.ArticleProcessing != nil {
		c := s.config.ArticleProcessing
		if c.MaxSegments > 0 {
			limit = c.MaxSegments
		}
		if c.SegmentOverlapChars > 0 {
			overlap = c.SegmentOverlapChars
		}
		if c.ContextReservePercent > 0 {
			reserve = c.ContextReservePercent
		}
	}
	return
}

// The same budget is used by planning and dispatch. MaxBodyChars only lowers
// a model's capacity; it never overrides the model context safety boundary.
func (s *entryService) segmentBudget(model *types.Model, prompt string, metadata any, images int) (int, error) {
	body, _, completion := s.articleProcessingLimits(model)
	_, overlap, reserve := s.segmentSettings()
	if model != nil && model.Parameters.ContextWindow > 0 {
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return 0, err
		}
		window := model.Parameters.ContextWindow
		body = min(body, window*(100-reserve)/100)
		body = min(body, window-completion-len([]rune(prompt))-len([]rune(string(encoded)))-images*1024-segmentEnvelopeReserve-overlap)
	}
	if body < 1 {
		return 0, errors.New("model context is too small for instructions, metadata and segment context")
	}
	return body, nil
}

func (s *entryService) openSegmentRun(ctx context.Context, entry *types.Entry, phase, body string, budget int, fingerprint any, legacyFingerprints ...any) (*entrySegmentRun, error) {
	limit, overlap, reserve := s.segmentSettings()
	plan, err := types.PlanProcessingSegments(body, budget, overlap, limit)
	if err != nil {
		return nil, err
	}
	if phase == "processing" {
		_, images, _ := s.articleProcessingLimits(nil)
		plan, err = imageBoundedSegments(body, plan, images, limit)
		if err != nil {
			return nil, err
		}
	}
	var policy any
	if s.config != nil {
		policy = s.config.ArticleProcessing
	}
	planKey := func(input any) string {
		return segmentHash([]any{types.ProcessingSegmentVersion, plan, input, budget, limit, overlap, reserve, policy, segmentGeneration(entry, phase)})
	}
	key := planKey(fingerprint)
	run := &entrySegmentRun{ID: uuid.NewString(), SpaceID: entry.SpaceID, EntryID: entry.ID, Phase: phase, PlanHash: key, BodyHash: fmt.Sprintf("%x", sha256.Sum256([]byte(body))), Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	checkpoints := make([]segmentCheckpoint, len(plan))
	for i, p := range plan {
		checkpoints[i] = segmentCheckpoint{ProcessingSegment: p, Status: "pending"}
	}
	raw, _ := json.Marshal(checkpoints)
	run.Plan = types.JSON(raw)
	if s.db == nil {
		return run, nil
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize plan creation without keeping a transaction across calls.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", entry.ID+":"+phase).Error; err != nil {
			return err
		}
		var latest entrySegmentRun
		err := tx.Where("entry_id=? AND space_id=? AND phase=?", entry.ID, entry.SpaceID, phase).Order("created_at DESC,id DESC").First(&latest).Error
		if err == nil {
			compatible := latest.PlanHash == key
			for _, fingerprint := range legacyFingerprints {
				compatible = compatible || latest.PlanHash == planKey(fingerprint)
			}
			if compatible {
				// Upgrade an unchanged legacy fingerprint without discarding checkpoints.
				if latest.PlanHash != key {
					if err := tx.Model(&latest).Update("plan_hash", key).Error; err != nil {
						return err
					}
					latest.PlanHash = key
				}
				*run = latest
				return nil
			}
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(run).Error
	})
	return run, err
}

// Each successful segment commits separately. The row lock also makes a
// concurrent retry re-read the winning checkpoint before calling the model.
func (s *entryService) mutateSegmentRun(ctx context.Context, run *entrySegmentRun, fn func(*entrySegmentRun) error) error {
	if s.db == nil {
		return fn(run)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", run.ID, run.SpaceID).First(run).Error; err != nil {
			return err
		}
		if err := fn(run); err != nil {
			return err
		}
		run.UpdatedAt = time.Now()
		return tx.Save(run).Error
	})
}

func (s *entryService) executeSegments(ctx context.Context, entry *types.Entry, run *entrySegmentRun, call func(context.Context, types.ProcessingSegment, int) (json.RawMessage, error)) ([]segmentCheckpoint, error) {
	var plan []segmentCheckpoint
	if err := json.Unmarshal(run.Plan, &plan); err != nil {
		return nil, err
	}
	for i := range plan {
		if aborted, _ := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
			return nil, context.Canceled
		}
		err := s.mutateSegmentRun(ctx, run, func(current *entrySegmentRun) error {
			if err := json.Unmarshal(current.Plan, &plan); err != nil {
				return err
			}
			p := &plan[i]
			if p.Status == "completed" {
				return nil
			}
			stage := types.StageProcessing
			if run.Phase == "review" {
				stage = types.StageDocReader
			}
			parent := s.tracker().LookupStage(ctx, entry.ID, attemptFromCtx(ctx), stage)
			span := s.tracker().BeginSubSpan(ctx, parent, fmt.Sprintf("%s.segment.%d/%d", run.Phase, i+1, len(plan)), types.SpanKindGeneration, types.JSONMap{"segment_number": i + 1, "segment_count": len(plan), "start": p.Start, "end": p.End, "run_id": run.ID})
			callCtx := ctx
			if span != nil {
				callCtx = types.WithModelCallTrace(ctx, span.Attempt, span.SpanID)
			}
			result, err := call(callCtx, p.ProcessingSegment, len(plan))
			if err != nil {
				s.tracker().FailSpan(ctx, span, "SEGMENT_FAILED", err.Error(), err)
				return err
			}
			p.Status, p.Result, p.Error = "completed", result, ""
			current.Status = "running"
			raw, err := json.Marshal(plan)
			if err != nil {
				return err
			}
			current.Plan = types.JSON(raw)
			s.tracker().EndSpan(ctx, span, types.JSONMap{"segment_number": i + 1, "segment_count": len(plan)})
			return nil
		})
		if err != nil {
			// Cancellation leaves the last committed plan usable on restart.
			if ctx.Err() == nil {
				_ = s.mutateSegmentRun(ctx, run, func(current *entrySegmentRun) error {
					var failed []segmentCheckpoint
					if e := json.Unmarshal(current.Plan, &failed); e != nil {
						return e
					}
					if failed[i].Status == "completed" {
						return nil
					}
					failed[i].Status, failed[i].Error = "failed", err.Error()
					raw, _ := json.Marshal(failed)
					current.Plan = types.JSON(raw)
					current.Status = "failed"
					return nil
				})
			}
			return nil, fmt.Errorf("第 %d/%d 段处理失败: %w", i+1, len(plan), err)
		}
	}
	return plan, nil
}

func segmentInput(p types.ProcessingSegment, count int) any {
	// Preserve full paths in the plan. Bound the repeated model context.
	path := append([]string{}, p.HeadingPath...)
	for i, h := range path {
		r := []rune(h)
		path[i] = string(r[:min(len(r), 128)])
	}
	return struct {
		Number        int      `json:"number"`
		Count         int      `json:"count"`
		Start         int      `json:"start"`
		End           int      `json:"end"`
		HeadingPath   []string `json:"heading_path"`
		ContextBefore string   `json:"context_before,omitempty"`
	}{p.Number, count, p.Start, p.End, path, p.ContextBefore}
}

func segmentModelIdentity(model *types.Model) any {
	if model == nil {
		return nil
	}
	return []any{model.ID, model.Name, model.Source, model.Parameters}
}
