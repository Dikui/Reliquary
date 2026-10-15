package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	task "reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestRetryTaskHTTPCancellationStopsFailedEntryAndRetainsHistory(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	db := inspector.db
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Cancellation"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "library", SpaceID: 1, Name: "Library"}).Error)
	require.NoError(t, db.Create(&types.Entry{ID: "retry-entry", SpaceID: 1, LibraryID: "library", Type: "manual", ParseStatus: types.ParseStatusPending}).Error)
	svc, err := service.NewEntryService(db, nil, repository.NewEntryRepository(db), nil, nil, nil, nil, nil, nil, nil, nil, inspector.client, inspector, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, inspector.client.Start(context.Background(), task.HandlerFunc(func(ctx context.Context, _ *task.Task) error {
		require.NoError(t, db.WithContext(ctx).Model(&types.Entry{}).Where("id=?", "retry-entry").Update("parse_status", types.ParseStatusFailed).Error)
		return errors.New("fixture model failure")
	}), task.Config{CheckInterval: 20 * time.Millisecond, RetryDelayFunc: func(int, error, *task.Task) time.Duration { return time.Hour }}))
	op, err := inspector.client.Enqueue(task.NewTask(types.TypeManualProcess, []byte(`{"space_id":1,"entry_id":"retry-entry","library_id":"library"}`)), task.MaxRetry(3))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var row task.Job
		return db.First(&row, "id=?", op.ID).Error == nil && row.State == task.TaskStateRetry
	}, 15*time.Second, 20*time.Millisecond)
	system := handler.NewSystemHandler(nil, nil, nil, nil, nil, inspector, svc)
	router := gin.New()
	router.POST("/:queue/:task_id/:action", system.MutateRuntimeTask)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/default/"+op.ID+"/cancel", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var row task.Job
	require.NoError(t, db.First(&row, "id=?", op.ID).Error)
	require.Equal(t, task.TaskStateCancelled, row.State)
	require.Equal(t, 1, row.FailCount)
	require.Equal(t, 1, row.Retried)
	var entry types.Entry
	require.NoError(t, db.First(&entry, "id=?", "retry-entry").Error)
	require.Equal(t, types.ParseStatusCancelled, entry.ParseStatus)
	info, _, err := inspector.GetRuntimeTask(context.Background(), types.QueueDefault, op.ID)
	require.NoError(t, err)
	require.Equal(t, []types.RuntimeTaskAction{types.RuntimeTaskActionDelete}, info.AllowedActions)
}

func TestRuntimeTaskAllStateActionsAndBusinessCancellationRestrictions(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	db := inspector.db
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Action matrix"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "library", SpaceID: 1, Name: "Library"}).Error)
	states := []task.TaskState{task.TaskStatePending, task.TaskStateDispatched, task.TaskStateActive, task.TaskStateScheduled, task.TaskStateRetry, task.TaskStateArchived, task.TaskStateCompleted, task.TaskStateCancelled}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			id := "matrix-" + string(state)
			require.NoError(t, db.Create(&types.Entry{ID: id, SpaceID: 1, LibraryID: "library", Type: "manual", ParseStatus: types.ParseStatusFailed}).Error)
			insertRuntimeJob(t, inspector, id, `{"space_id":1,"entry_id":"`+id+`"}`, types.QueueDefault, types.RuntimeTaskState(state), time.Now())
			info, _, err := inspector.GetRuntimeTask(context.Background(), types.QueueDefault, id)
			require.NoError(t, err)
			want := []types.RuntimeTaskAction{}
			switch state {
			case task.TaskStatePending, task.TaskStateDispatched, task.TaskStateActive:
				want = append(want, types.RuntimeTaskActionCancel)
			case task.TaskStateScheduled, task.TaskStateRetry:
				want = append(want, types.RuntimeTaskActionCancel, types.RuntimeTaskActionRunNow)
			case task.TaskStateArchived:
				want = append(want, types.RuntimeTaskActionRunNow, types.RuntimeTaskActionDelete)
			case task.TaskStateCancelled:
				want = append(want, types.RuntimeTaskActionDelete)
			}
			require.Equal(t, want, info.AllowedActions)
			if info.Allows(types.RuntimeTaskActionCancel) {
				for _, status := range []string{types.ParseStatusCompleted, types.ParseStatusDeleting} {
					require.NoError(t, db.Model(&types.Entry{}).Where("id=?", id).Update("parse_status", status).Error)
					got, _, err := inspector.GetRuntimeTask(context.Background(), types.QueueDefault, id)
					require.NoError(t, err)
					require.False(t, got.Allows(types.RuntimeTaskActionCancel))
					require.Equal(t, status, got.CancellationUnavailableReason)
					page, _, err := inspector.ListRuntimeTasks(context.Background(), types.QueueDefault, got.State, "", 100)
					require.NoError(t, err)
					for _, listed := range page.Tasks {
						if listed.ID == id {
							require.Equal(t, got.AllowedActions, listed.AllowedActions)
							require.Equal(t, status, listed.CancellationUnavailableReason)
						}
					}
				}
			}
		})
	}
}
