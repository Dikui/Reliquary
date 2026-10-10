package service

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type nameEmbeddingTriggerCall struct {
	mark     bool
	tenantID uint64
	reason   string
}

type nameEmbeddingTriggerSpy struct {
	calls  []nameEmbeddingTriggerCall
	err    error
	onCall func()
}

func (s *nameEmbeddingTriggerSpy) call(mark bool, tenantID uint64, reason string) error {
	if s.onCall != nil {
		s.onCall()
	}
	s.calls = append(s.calls, nameEmbeddingTriggerCall{mark: mark, tenantID: tenantID, reason: reason})
	return s.err
}
func (s *nameEmbeddingTriggerSpy) MarkAndEnqueue(_ context.Context, tenantID uint64, reason string) error {
	return s.call(true, tenantID, reason)
}
func (s *nameEmbeddingTriggerSpy) Enqueue(_ context.Context, tenantID uint64, reason string) error {
	return s.call(false, tenantID, reason)
}

func nameEmbeddingTriggerDB(t *testing.T) (*gorm.DB, context.Context) {
	t.Helper()
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	team := types.Tenant{ID: 1, Name: "Rebuild triggers"}
	require.NoError(t, db.Create(&team).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "trigger-kb", TenantID: 1, Name: "Knowledge Base", EmbeddingModelID: "team-vector"}).Error)
	for _, id := range []string{"team-vector", "override-vector"} {
		require.NoError(t, db.Create(&types.Model{ID: id, TenantID: 1, Name: id, Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3}}}).Error)
	}
	return db, ctx
}

func TestKnowledgeBaseNameEmbeddingTriggersAfterCommittedChanges(t *testing.T) {
	db, ctx := nameEmbeddingTriggerDB(t)
	kb := &types.KnowledgeBase{}
	require.NoError(t, db.Where("tenant_id=1").First(kb).Error)
	spy := &nameEmbeddingTriggerSpy{}
	svc := &knowledgeBaseService{db: db, repo: repository.NewKnowledgeBaseRepository(db)}
	svc.SetNameEmbeddingRebuilder(spy)
	config := &types.KnowledgeBaseConfig{ModelTiers: &types.ModelTierConfig{Embedding: "override-vector"}}
	spy.onCall = func() {
		var saved types.KnowledgeBase
		require.NoError(t, db.First(&saved, "id = ?", kb.ID).Error)
		require.Equal(t, config.ModelTiers.Embedding, saved.ModelTiers.Embedding, "enqueue sees committed configuration")
	}
	_, err := svc.UpdateKnowledgeBase(ctx, kb.ID, kb.Name, "", config)
	require.NoError(t, err)
	require.Equal(t, []nameEmbeddingTriggerCall{{true, 1, "knowledge_base_embedding_model_changed"}}, spy.calls)
	spy.calls = nil
	_, err = svc.UpdateKnowledgeBase(ctx, kb.ID, kb.Name, "rename metadata", config)
	require.NoError(t, err)
	require.Empty(t, spy.calls)
	config.ModelTiers.Embedding = ""
	spy.err = errors.New("queue unavailable")
	saved, err := svc.UpdateKnowledgeBase(ctx, kb.ID, kb.Name, "", config)
	require.ErrorContains(t, err, "knowledge base saved")
	require.NotNil(t, saved)
	require.Equal(t, []nameEmbeddingTriggerCall{{true, 1, "knowledge_base_embedding_model_changed"}}, spy.calls)
}

func TestEmbeddingModelUpdateTriggersCurrentTeamProfileOnly(t *testing.T) {
	db, ctx := nameEmbeddingTriggerDB(t)
	svc := NewModelServiceWithRoutingDB(repository.NewModelRepository(db), nil, nil, nil, nil, db).(*modelService)
	spy := &nameEmbeddingTriggerSpy{}
	svc.SetNameEmbeddingRebuilder(spy)
	var model types.Model
	require.NoError(t, db.First(&model, "id = ?", "team-vector").Error)
	model.DisplayName = "Cosmetic change"
	require.NoError(t, svc.UpdateModel(ctx, &model))
	require.Empty(t, spy.calls)
	model.Parameters.APIKey = "credential-change"
	model.Parameters.MaxConcurrency = 2
	require.NoError(t, svc.UpdateModel(ctx, &model))
	require.Empty(t, spy.calls, "credentials and request budgets do not change vector semantics")
	model.Parameters.EmbeddingParameters.Dimension = 4
	spy.onCall = func() {
		var saved types.Model
		require.NoError(t, db.First(&saved, "id = ?", model.ID).Error)
		require.Equal(t, 4, saved.Parameters.EmbeddingParameters.Dimension)
	}
	spy.err = errors.New("queue disconnected")
	err := svc.UpdateModel(ctx, &model)
	require.ErrorContains(t, err, "model saved")
	require.Equal(t, []nameEmbeddingTriggerCall{{true, 1, "embedding_model_updated"}}, spy.calls)
	spy.calls, spy.err, spy.onCall = nil, nil, nil
	model = types.Model{}
	require.NoError(t, db.First(&model, "id = ?", "override-vector").Error)
	model.Parameters.EmbeddingParameters.Dimension = 4
	require.NoError(t, svc.UpdateModel(ctx, &model))
	require.Empty(t, spy.calls, "an unused model does not change the entity profile")
}

type nameEmbeddingTriggerQueue struct{ tasks []*asynq.Task }

func (q *nameEmbeddingTriggerQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: "name-rebuild", Type: task.Type()}, nil
}

type nameEmbeddingTriggerModels struct {
	interfaces.ModelService
	selected []string
	embedder *nameEmbeddingTriggerEmbedder
}

func (m *nameEmbeddingTriggerModels) GetEmbeddingModel(_ context.Context, id string) (embedding.Embedder, error) {
	m.selected = append(m.selected, id)
	return m.embedder, nil
}

type nameEmbeddingTriggerEmbedder struct {
	embedding.Embedder
	calls int
}

func (*nameEmbeddingTriggerEmbedder) GetDimensions() int { return 3 }
func (*nameEmbeddingTriggerEmbedder) GetModelID() string { return "team-vector" }
func (e *nameEmbeddingTriggerEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0, 0}
	}
	return vectors, nil
}

func TestNameEmbeddingConfigurationAndEmptyWorkbenchIntegrateWithDurableTask(t *testing.T) {
	db, ctx := nameEmbeddingTriggerDB(t)
	entity := types.Entity{ID: "historical-name", TenantID: 1, CanonicalName: "Historical entity", NormalizedName: "historical entity", EntityType: "company", Description: "Existing entity", NameEmbedding: types.EntityNameEmbedding{0, 1, 0}, EmbeddingModelID: "retired-vector", EmbeddingDimension: 3, EmbeddingNeedsRebuild: false}
	require.NoError(t, db.Create(&entity).Error)
	queue := &nameEmbeddingTriggerQueue{}
	models := &nameEmbeddingTriggerModels{embedder: &nameEmbeddingTriggerEmbedder{}}
	rebuilder := NewEntityNameEmbeddingRebuilder(db, models, queue)
	kb := &types.KnowledgeBase{}
	require.NoError(t, db.Where("tenant_id=1").First(kb).Error)
	bases := &knowledgeBaseService{db: db, repo: repository.NewKnowledgeBaseRepository(db)}
	bases.SetNameEmbeddingRebuilder(rebuilder)
	_, err := bases.UpdateKnowledgeBase(ctx, kb.ID, kb.Name, "", &types.KnowledgeBaseConfig{ModelTiers: &types.ModelTierConfig{Embedding: "override-vector"}})
	require.NoError(t, err)
	require.Len(t, queue.tasks, 1)
	require.Equal(t, types.TypeEntityNameEmbeddingRebuild, queue.tasks[0].Type())
	require.NoError(t, db.First(&entity, "id = ?", entity.ID).Error)
	require.True(t, entity.EmbeddingNeedsRebuild)
	require.NoError(t, rebuilder.Handle(ctx, queue.tasks[0]))
	require.NoError(t, db.First(&entity, "id = ?", entity.ID).Error)
	require.False(t, entity.EmbeddingNeedsRebuild)
	require.Equal(t, "override-vector", entity.EmbeddingModelID)
	require.Equal(t, 1, models.embedder.calls)
	for _, selected := range models.selected {
		require.Equal(t, "override-vector", selected, "entity names use the knowledge base profile")
	}
	require.NoError(t, db.Model(&entity).Update("embedding_needs_rebuild", true).Error)
	require.NoError(t, rebuilder.Enqueue(ctx, 1, "workbench_rebuild"))
	require.Len(t, queue.tasks, 2)
	require.NoError(t, rebuilder.Handle(ctx, queue.tasks[1]))
	status, err := rebuilder.Status(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, status.PendingCount)
	require.Equal(t, "completed", status.LatestRun.Status)
	require.Equal(t, "workbench_rebuild", status.LatestRun.Reason)
	require.EqualValues(t, 1, status.LatestRun.ProcessedCount)
	require.Equal(t, 2, models.embedder.calls)
}
