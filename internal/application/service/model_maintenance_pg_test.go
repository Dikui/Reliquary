package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func maintenanceModelFixture(t *testing.T, fields []string) (*modelService, context.Context, *types.Model) {
	t.Helper()
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	model := &types.Model{ID: "migration-paused-model", SpaceID: 1, Name: "fixture", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceRemote, Status: types.ModelStatusNeedsConfiguration, Parameters: types.ModelParameters{InterfaceType: "openai", ExtraConfig: map[string]string{maintenanceRequiredFields: string(encoded)}}}
	require.NoError(t, db.Create(model).Error)
	s := NewModelServiceWithRoutingDB(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, db).(*modelService)
	return s, ctx, model
}

func TestMigrationPausedModelStaysEditableButRuntimeCannotCallIt(t *testing.T) {
	s, ctx, model := maintenanceModelFixture(t, []string{"api_key", "app_secret"})
	editable, err := s.GetModelByID(ctx, model.ID)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusNeedsConfiguration, editable.Status)
	adapter, err := s.GetChatModel(ctx, model.ID)
	require.Error(t, err)
	require.Nil(t, adapter)
	var unavailable *apperrors.AppError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, apperrors.ErrServiceUnavailable, unavailable.Code)
	key := "fixture-reentered-api-key"
	updated, err := s.UpdateModelCredentials(ctx, model.ID, &key, nil)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusNeedsConfiguration, updated.Status)
	secret := "fixture-reentered-app-secret"
	updated, err = s.UpdateModelCredentials(ctx, model.ID, nil, &secret)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusActive, updated.Status)
	require.NotContains(t, updated.Parameters.ExtraConfig, maintenanceRequiredFields)
	adapter, err = s.GetChatModel(ctx, model.ID)
	require.NoError(t, err)
	require.NotNil(t, adapter)
}

func TestMigrationModelNeedsAllCustomHeaderFieldsAndCannotDropTheMarker(t *testing.T) {
	s, ctx, model := maintenanceModelFixture(t, []string{"api_key", "custom_headers.X-Gateway-Token"})
	key := "fixture-reentered-api-key"
	_, err := s.UpdateModelCredentials(ctx, model.ID, &key, nil)
	require.NoError(t, err)
	updated, err := s.GetModelByID(ctx, model.ID)
	require.NoError(t, err)
	updated.Status = types.ModelStatusActive
	updated.Parameters.ExtraConfig = map[string]string{}
	require.NoError(t, s.UpdateModel(ctx, updated))
	stored, err := s.repo.GetByID(ctx, 1, model.ID)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusNeedsConfiguration, stored.Status)
	require.NotEmpty(t, stored.Parameters.ExtraConfig[maintenanceRequiredFields])
	stored.Parameters.CustomHeaders = map[string]string{"x-gateway-token": "fixture-reentered-header"}
	require.NoError(t, s.UpdateModel(ctx, stored))
	stored, err = s.repo.GetByID(ctx, 1, model.ID)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusActive, stored.Status)
	require.NotContains(t, stored.Parameters.ExtraConfig, maintenanceRequiredFields)
}

func TestMigrationChatGPTModelActivatesOnlyAfterAccountLogin(t *testing.T) {
	s, ctx, model := maintenanceModelFixture(t, []string{"chatgpt_login"})
	accountID := "00000000-0000-0000-0000-000000000015"
	require.NoError(t, s.db.Exec("INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,status) VALUES(?, 'migration-fixture','https://example.invalid','fixture','needs_login')", accountID).Error)
	model.Source = types.ModelSourceChatGPT
	model.Parameters.ExtraConfig["chatgpt_account_id"] = accountID
	require.NoError(t, s.repo.Update(ctx, model))
	models, err := s.ListModels(ctx)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, types.ModelStatusNeedsConfiguration, models[0].Status)
	require.NoError(t, s.db.Exec("UPDATE chatgpt_accounts SET status='connected',credentials='fixture-encrypted-credential-presence' WHERE id=?", accountID).Error)
	models, err = s.ListModels(ctx)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusActive, models[0].Status)
	require.NotContains(t, models[0].Parameters.ExtraConfig, maintenanceRequiredFields)
}

func TestMaintenanceURLCredentialMarkersRequireTheRemovedURLParts(t *testing.T) {
	parameters := types.ModelParameters{BaseURL: "https://example.invalid/v1"}
	require.False(t, maintenanceFieldConfigured(parameters, "base_url.userinfo"))
	require.False(t, maintenanceFieldConfigured(parameters, "base_url.query.api_key"))
	parameters.BaseURL = "https://user:fixture@example.invalid/v1?api_key=fixture"
	require.True(t, maintenanceFieldConfigured(parameters, "base_url.userinfo"))
	require.True(t, maintenanceFieldConfigured(parameters, "base_url.query.api_key"))
}

func TestMigrationModelRestoresOriginalStatusAndPreservesProvenanceThroughEdits(t *testing.T) {
	s, ctx, model := maintenanceModelFixture(t, []string{"api_key", "custom_headers.Authorization"})
	const originalUpdatedAt = "2026-10-10T12:00:00+00:00"
	model.Parameters.ExtraConfig[maintenanceOriginalStatus] = string(types.ModelStatusDownloadFailed)
	model.Parameters.ExtraConfig[maintenanceOriginalUpdatedAt] = originalUpdatedAt
	require.NoError(t, s.repo.Update(ctx, model))
	key := "fixture-reentered-api-key"
	_, err := s.UpdateModelCredentials(ctx, model.ID, &key, nil)
	require.NoError(t, err)
	updated, err := s.GetModelByID(ctx, model.ID)
	require.NoError(t, err)
	updated.Status = types.ModelStatusActive
	updated.Parameters.ExtraConfig = map[string]string{maintenanceOriginalStatus: "active", maintenanceOriginalUpdatedAt: "forged"}
	require.NoError(t, s.UpdateModel(ctx, updated))
	stored, err := s.repo.GetByID(ctx, 1, model.ID)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusNeedsConfiguration, stored.Status)
	require.Equal(t, string(types.ModelStatusDownloadFailed), stored.Parameters.ExtraConfig[maintenanceOriginalStatus])
	require.Equal(t, originalUpdatedAt, stored.Parameters.ExtraConfig[maintenanceOriginalUpdatedAt])
	stored.Parameters.CustomHeaders = map[string]string{"authorization": "fixture-reentered-header"}
	require.NoError(t, s.UpdateModel(ctx, stored))
	stored, err = s.repo.GetByID(ctx, 1, model.ID)
	require.NoError(t, err)
	require.Equal(t, types.ModelStatusDownloadFailed, stored.Status)
	require.NotContains(t, stored.Parameters.ExtraConfig, maintenanceRequiredFields)
	require.Equal(t, string(types.ModelStatusDownloadFailed), stored.Parameters.ExtraConfig[maintenanceOriginalStatus])
	require.Equal(t, originalUpdatedAt, stored.Parameters.ExtraConfig[maintenanceOriginalUpdatedAt])
	stored.Parameters.ExtraConfig = map[string]string{}
	require.NoError(t, s.UpdateModel(ctx, stored))
	stored, err = s.repo.GetByID(ctx, 1, model.ID)
	require.NoError(t, err)
	require.Equal(t, originalUpdatedAt, stored.Parameters.ExtraConfig[maintenanceOriginalUpdatedAt])
}
