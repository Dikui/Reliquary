package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type adminRouteSettings struct {
	interfaces.SystemSettingService
}

func (adminRouteSettings) List(context.Context) ([]*types.SystemSetting, error) {
	return []*types.SystemSetting{{Key: "file.max_size_mb", Value: types.JSON("50"), ValueType: "int"}}, nil
}
func (adminRouteSettings) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	return &types.SystemSetting{Key: key, Value: types.JSON("50"), ValueType: "int"}, nil
}
func (adminRouteSettings) Update(_ context.Context, key string, value any) (*types.SystemSetting, error) {
	encoded, err := json.Marshal(value)
	return &types.SystemSetting{Key: key, Value: types.JSON(encoded), ValueType: "int"}, err
}
func (adminRouteSettings) Reset(context.Context, string) error { return nil }
func (adminRouteSettings) GetInt(_ context.Context, _, _ string, fallback int64) int64 {
	return fallback
}

type adminRouteInspector struct {
	interfaces.TaskInspector
	interfaces.RuntimeTaskInspector
}

func (adminRouteInspector) QueueStats(context.Context) ([]types.QueueStat, bool, error) {
	return []types.QueueStat{{Name: types.QueueDefault}}, true, nil
}
func (adminRouteInspector) WorkerServerStats(context.Context) ([]types.WorkerServerStat, bool, error) {
	return nil, true, nil
}
func (adminRouteInspector) ListRuntimeTasks(context.Context, string, types.RuntimeTaskState, string, int) (types.RuntimeTaskPage, bool, error) {
	return types.RuntimeTaskPage{}, true, nil
}
func (adminRouteInspector) GetRuntimeTask(_ context.Context, queue, id string) (*types.RuntimeTaskInfo, bool, error) {
	return &types.RuntimeTaskInfo{ID: id, Queue: queue, State: types.RuntimeTaskArchived, AllowedActions: []types.RuntimeTaskAction{types.RuntimeTaskActionDelete}}, true, nil
}
func (adminRouteInspector) DeleteRuntimeTask(context.Context, string, string) (bool, error) {
	return true, nil
}
func (adminRouteInspector) PurgeArchivedRuntimeTasks(context.Context, string) (int, bool, error) {
	return 1, true, nil
}

func TestRestoredSystemAdminRoutesRequireSystemAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enforced := range []bool{true, false} {
		cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enforced}}
		engine := gin.New()
		engine.Use(func(c *gin.Context) {
			ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, types.SingleTenantID)
			ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRole(c.GetHeader("X-Test-Role")))
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, c.GetHeader("X-Test-System-Admin") == "true")
			if c.GetHeader("X-Test-API-Key") == "true" {
				ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{FullAccess: true})
			}
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		})
		h := handler.NewSystemHandler(cfg, nil, nil, nil, adminRouteSettings{}, nil, nil, adminRouteInspector{}, nil, nil, nil)
		guards := newRBACGuards(cfg, nil, nil, nil, nil, nil, nil, nil, nil)
		RegisterSystemAdminRoutes(engine.Group("/api/v1"), h, nil, guards)
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
				name, role          string
				systemAdmin, apiKey bool
				status              int
			}{
				{"system admin", "viewer", true, false, http.StatusOK},
				{"owner", "owner", false, false, http.StatusForbidden},
				{"admin", "admin", false, false, http.StatusForbidden},
				{"contributor", "contributor", false, false, http.StatusForbidden},
				{"viewer", "viewer", false, false, http.StatusForbidden},
				{"team API key", "owner", true, true, http.StatusForbidden},
			} {
				t.Run(route.method+route.path+"/"+actor.name+"/rbac="+map[bool]string{true: "true", false: "false"}[enforced], func(t *testing.T) {
					req := httptest.NewRequest(route.method, "/api/v1/system/admin"+route.path, strings.NewReader(route.body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Test-Role", actor.role)
					if actor.systemAdmin {
						req.Header.Set("X-Test-System-Admin", "true")
					}
					if actor.apiKey {
						req.Header.Set("X-Test-API-Key", "true")
					}
					rec := httptest.NewRecorder()
					engine.ServeHTTP(rec, req)
					require.Equal(t, actor.status, rec.Code, rec.Body.String())
				})
			}
		}
	}
}
