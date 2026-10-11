package service

import (
	"context"
	"errors"
	"sync"

	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type kbDeleteDSRepo struct {
	mu        sync.Mutex
	byKB      map[string][]*types.DataSource
	deleted   map[string]bool
	deleteIDs []string
}

func newKBDeleteDSRepo(kbID string, ds ...*types.DataSource) *kbDeleteDSRepo {
	r := &kbDeleteDSRepo{
		byKB:    map[string][]*types.DataSource{kbID: ds},
		deleted: map[string]bool{},
	}
	return r
}

func (r *kbDeleteDSRepo) Create(_ context.Context, _ *types.DataSource) error { return nil }
func (r *kbDeleteDSRepo) FindByID(_ context.Context, id string) (*types.DataSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleted[id] {
		return nil, errors.New("data source not found")
	}
	for _, list := range r.byKB {
		for _, ds := range list {
			if ds.ID == id {
				return ds, nil
			}
		}
	}
	return nil, errors.New("data source not found")
}
func (r *kbDeleteDSRepo) FindByKnowledgeBase(_ context.Context, kbID string) ([]*types.DataSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var active []*types.DataSource
	for _, ds := range r.byKB[kbID] {
		if !r.deleted[ds.ID] {
			active = append(active, ds)
		}
	}
	return active, nil
}
func (r *kbDeleteDSRepo) Update(_ context.Context, _ *types.DataSource) error { return nil }
func (r *kbDeleteDSRepo) UpdateSyncState(_ context.Context, _ *types.DataSource) error {
	return nil
}
func (r *kbDeleteDSRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted[id] = true
	r.deleteIDs = append(r.deleteIDs, id)
	return nil
}
func (r *kbDeleteDSRepo) FindActive(_ context.Context) ([]*types.DataSource, error) {
	return nil, nil
}

var _ interfaces.DataSourceRepository = (*kbDeleteDSRepo)(nil)

type kbDeleteSyncLogRepo struct {
	mu       sync.Mutex
	canceled []string
}

func (r *kbDeleteSyncLogRepo) Create(_ context.Context, _ *types.SyncLog) error { return nil }
func (r *kbDeleteSyncLogRepo) FindByID(_ context.Context, _ string) (*types.SyncLog, error) {
	return nil, errors.New("not found")
}
func (r *kbDeleteSyncLogRepo) FindByDataSource(_ context.Context, _ string, _, _ int) ([]*types.SyncLog, error) {
	return nil, nil
}
func (r *kbDeleteSyncLogRepo) FindLatest(_ context.Context, _ string) (*types.SyncLog, error) {
	return nil, nil
}
func (r *kbDeleteSyncLogRepo) HasRunningSync(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (r *kbDeleteSyncLogRepo) Update(_ context.Context, _ *types.SyncLog) error { return nil }
func (r *kbDeleteSyncLogRepo) UpdateResult(_ context.Context, _ *types.SyncLog) error {
	return nil
}
func (r *kbDeleteSyncLogRepo) CancelPendingByDataSource(_ context.Context, dsID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.canceled = append(r.canceled, dsID)
	return nil
}
func (r *kbDeleteSyncLogRepo) CleanupOldLogs(_ context.Context, _ int) error { return nil }

var _ interfaces.SyncLogRepository = (*kbDeleteSyncLogRepo)(nil)

type kbDeleteTaskEnqueuer struct{}

func (kbDeleteTaskEnqueuer) Enqueue(_ *taskqueue.Task, _ ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return &taskqueue.TaskInfo{ID: "kb-delete-task"}, nil
}
