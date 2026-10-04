package service

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"testing"
	"time"

	"reliquary/internal/config"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type evaluationSnapshotDataset struct {
	interfaces.DatasetService
	started chan struct{}
	release chan struct{}
}

func (d evaluationSnapshotDataset) GetDatasetByID(context.Context, string) ([]*types.QAPair, error) {
	close(d.started)
	<-d.release
	pairs := make([]*types.QAPair, 8)
	for i := range pairs {
		pairs[i] = &types.QAPair{Question: "question", PIDs: []int{0}, Passages: []string{"source"}, Answer: "answer"}
	}
	return pairs, nil
}

type evaluationSnapshotKnowledge struct{ interfaces.KnowledgeService }

func (evaluationSnapshotKnowledge) CreateKnowledgeFromPassageSync(context.Context, string, []string, string) (*types.Knowledge, error) {
	return &types.Knowledge{ID: "evaluation-knowledge"}, nil
}
func (evaluationSnapshotKnowledge) DeleteKnowledge(context.Context, string) error { return nil }

type evaluationSnapshotKB struct {
	interfaces.KnowledgeBaseService
}

func (evaluationSnapshotKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{}, nil
}
func (evaluationSnapshotKB) CreateKnowledgeBase(context.Context, *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "evaluation-kb"}, nil
}
func (evaluationSnapshotKB) DeleteKnowledgeBase(context.Context, string) error { return nil }

type evaluationSnapshotSession struct {
	interfaces.SessionService
	err error
}

func (s evaluationSnapshotSession) KnowledgeQAByEvent(_ context.Context, params *types.ChatManage, _ []types.EventType) error {
	if params.ChatModelID != "model" {
		return errors.New("caller changed evaluation parameters")
	}
	runtime.Gosched()
	return s.err
}

func TestEvaluationProgressSnapshots(t *testing.T) {
	for _, qaErr := range []error{nil, errors.New("QA failed")} {
		name := "success"
		if qaErr != nil {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
			dataset := evaluationSnapshotDataset{started: make(chan struct{}), release: make(chan struct{})}
			cfg := &config.Config{Conversation: &config.ConversationConfig{Summary: &config.SummaryConfig{}}}
			svc := NewEvaluationService(cfg, dataset, evaluationSnapshotKB{}, evaluationSnapshotKnowledge{}, evaluationSnapshotSession{err: qaErr}, nil)
			initial, err := svc.Evaluation(ctx, name, "source-kb", "model", "rerank")
			if err != nil {
				t.Fatal(err)
			}
			<-dataset.started
			before, err := svc.EvaluationResult(ctx, initial.Task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if initial.Task.Status != types.EvaluationStatuePending || before.Task.Status != types.EvaluationStatueRunning {
				t.Fatalf("initial/running snapshots: %v/%v", initial.Task.Status, before.Task.Status)
			}
			initial.Params.ChatModelID = "caller mutation"
			close(dataset.release)

			deadline := time.Now().Add(5 * time.Second)
			var final *types.EvaluationDetail
			for time.Now().Before(deadline) {
				final, err = svc.EvaluationResult(ctx, initial.Task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := json.Marshal(final); err != nil {
					t.Fatal(err)
				}
				if final.Task.Finished > final.Task.Total || final.Params.ChatModelID != "model" {
					t.Fatalf("inconsistent progress snapshot: %+v", final.Task)
				}
				if final.Task.Status == types.EvaluationStatueSuccess || final.Task.Status == types.EvaluationStatueFailed {
					break
				}
				runtime.Gosched()
			}
			if qaErr != nil {
				if final.Task.Status != types.EvaluationStatueFailed || final.Task.ErrMsg != qaErr.Error() {
					t.Fatalf("failed snapshot: %+v", final.Task)
				}
			} else {
				if final.Task.Status != types.EvaluationStatueSuccess || final.Task.Total != 8 || final.Task.Finished != 8 || final.Metric == nil {
					t.Fatalf("completed snapshot: %+v", final.Task)
				}
				final.Metric.RetrievalMetrics.Precision = -1
				current, err := svc.EvaluationResult(ctx, initial.Task.ID)
				if err != nil || current.Metric.RetrievalMetrics.Precision == -1 {
					t.Fatalf("caller mutated stored metrics: %+v, %v", current, err)
				}
			}
			if before.Task.Total != 0 || before.Task.Finished != 0 || before.Task.Status != types.EvaluationStatueRunning {
				t.Fatalf("previous snapshot changed: %+v", before.Task)
			}
		})
	}
}
