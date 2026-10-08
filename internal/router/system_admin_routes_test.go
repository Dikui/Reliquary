package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type personalSystemRouteSettings struct {
	interfaces.SystemSettingService
}

func (personalSystemRouteSettings) List(context.Context) ([]*types.SystemSetting, error) {
	return []*types.SystemSetting{{Key: "file.max_size_mb", Value: types.JSON("50"), ValueType: "int"}}, nil
}
func (personalSystemRouteSettings) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	return &types.SystemSetting{Key: key, Value: types.JSON("50"), ValueType: "int"}, nil
}
func (personalSystemRouteSettings) Update(_ context.Context, key string, value any) (*types.SystemSetting, error) {
	encoded, err := json.Marshal(value)
	return &types.SystemSetting{Key: key, Value: types.JSON(encoded), ValueType: "int"}, err
}
func (personalSystemRouteSettings) Reset(context.Context, string) error { return nil }
func (personalSystemRouteSettings) GetInt(_ context.Context, _, _ string, fallback int64) int64 {
	return fallback
}

type personalSystemRouteInspector struct {
	interfaces.TaskInspector
	interfaces.RuntimeTaskInspector
}

func (personalSystemRouteInspector) QueueStats(context.Context) ([]types.QueueStat, bool, error) {
	return []types.QueueStat{{Name: types.QueueDefault}}, true, nil
}
func (personalSystemRouteInspector) WorkerServerStats(context.Context) ([]types.WorkerServerStat, bool, error) {
	return nil, true, nil
}
func (personalSystemRouteInspector) ListRuntimeTasks(context.Context, string, types.RuntimeTaskState, string, int) (types.RuntimeTaskPage, bool, error) {
	return types.RuntimeTaskPage{}, true, nil
}
func (personalSystemRouteInspector) GetRuntimeTask(_ context.Context, queue, id string) (*types.RuntimeTaskInfo, bool, error) {
	return &types.RuntimeTaskInfo{ID: id, TenantID: types.SingleTenantID, Queue: queue, State: types.RuntimeTaskArchived, AllowedActions: []types.RuntimeTaskAction{types.RuntimeTaskActionDelete}}, true, nil
}
func (personalSystemRouteInspector) DeleteRuntimeTask(context.Context, string, string) (bool, error) {
	return true, nil
}
func (personalSystemRouteInspector) PurgeArchivedRuntimeTasks(context.Context, string) (int, bool, error) {
	return 1, true, nil
}

func TestPersonalUserAndAPIKeyCanAccessAllSystemRoutes(t *testing.T) {
	f := newPersonalAuthFixture(t)
	cfg := &config.Config{Tenant: &config.TenantConfig{}}
	tenantSvc := service.NewTenantService(apprepo.NewTenantRepository(f.db))
	users := service.NewUserService(cfg, apprepo.NewUserRepository(f.db), apprepo.NewAuthTokenRepository(f.db), tenantSvc)
	keys := service.NewTenantAPIKeyService(apprepo.NewTenantAPIKeyRepository(f.db))
	h := handler.NewSystemHandler(cfg, tenantSvc, users, personalSystemRouteSettings{}, keys, personalSystemRouteInspector{}, nil, nil)
	r := gin.New()
	r.Use(middleware.ErrorHandler(), middleware.Auth(tenantSvc, users, keys))
	RegisterSystemAdminRoutes(r.Group("/api/v1"), h, newAccessGuards(nil, nil, nil))

	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/settings", ""},
		{http.MethodGet, "/settings/file.max_size_mb", ""},
		{http.MethodPut, "/settings/file.max_size_mb", `{"value":100}`},
		{http.MethodDelete, "/settings/file.max_size_mb", ""},
		{http.MethodGet, "/runtime/queues", ""},
		{http.MethodGet, "/runtime/queues/default/tasks?state=archived", ""},
		{http.MethodPost, "/runtime/queues/default/tasks/task/actions/delete", ""},
		{http.MethodDelete, "/runtime/queues/default/archived", ""},
	} {
		for _, actor := range []struct {
			name, credential string
			apiKey           bool
		}{
			{"login", f.token, false},
			{"API key", f.key, true},
		} {
			t.Run(route.method+route.path+"/"+actor.name, func(t *testing.T) {
				response := personalRequest(t, r, route.method, "/api/v1/system/admin"+route.path, route.body, actor.credential, actor.apiKey)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			})
		}
	}
}
