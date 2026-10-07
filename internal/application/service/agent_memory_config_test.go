package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	"reliquary/internal/types"
)

// buildAgentConfig copies CustomAgentConfig into the runtime AgentConfig field
// by field, and MemoryEnabled was missing from that list. Nothing failed
// loudly: the agent path read a nil preference and treated every agent as
// inheriting the workspace, so an agent explicitly barred from memory still
// got the user's memories injected into its prompt.
func TestAgentConfigCarriesTheMemoryPreference(t *testing.T) {
	for _, tc := range []struct {
		name string
		want *bool
	}{
		{name: "opted out", want: boolPtr(false)},
		{name: "opted in", want: boolPtr(true)},
		{name: "inherits the workspace", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &sessionService{
				cfg:                   &config.Config{},
				webSearchProviderRepo: &agentWebSearchRepo{},
			}
			req := &types.QARequest{
				Session: &types.Session{ID: "session-1", TenantID: 1},
				CustomAgent: &types.CustomAgent{
					TenantID: 1,
					Config: types.CustomAgentConfig{
						MaxIterations: 5,
						MemoryEnabled: tc.want,
					},
				},
			}

			agentConfig, err := svc.buildAgentConfig(t.Context(), req, &types.Tenant{ID: 1}, 1)
			require.NoError(t, err)
			require.Equal(t, tc.want, agentConfig.MemoryEnabled)
		})
	}
}

func TestAgentConfigCarriesMaxCompletionTokens(t *testing.T) {
	svc := &sessionService{
		cfg:                   &config.Config{},
		webSearchProviderRepo: &agentWebSearchRepo{},
	}
	req := &types.QARequest{
		Session: &types.Session{ID: "session-1", TenantID: 1},
		CustomAgent: &types.CustomAgent{
			TenantID: 1,
			Config: types.CustomAgentConfig{
				MaxIterations:       5,
				MaxCompletionTokens: 64000,
			},
		},
	}

	agentConfig, err := svc.buildAgentConfig(t.Context(), req, &types.Tenant{ID: 1}, 1)
	require.NoError(t, err)
	require.Equal(t, 64000, agentConfig.MaxCompletionTokens)
}

func boolPtr(v bool) *bool { return &v }

type agentWebSearchRepo struct {
	byIDTenant      uint64
	byID            string
	defaultTenant   uint64
	explicit        *types.WebSearchProviderEntity
	defaultProvider *types.WebSearchProviderEntity
}

func (r *agentWebSearchRepo) Create(context.Context, *types.WebSearchProviderEntity) error {
	return nil
}
func (r *agentWebSearchRepo) GetByID(_ context.Context, tenantID uint64, id string) (*types.WebSearchProviderEntity, error) {
	r.byIDTenant = tenantID
	r.byID = id
	return r.explicit, nil
}
func (r *agentWebSearchRepo) GetDefault(_ context.Context, tenantID uint64) (*types.WebSearchProviderEntity, error) {
	r.defaultTenant = tenantID
	return r.defaultProvider, nil
}
func (r *agentWebSearchRepo) List(context.Context, uint64) ([]*types.WebSearchProviderEntity, error) {
	return nil, nil
}
func (r *agentWebSearchRepo) Update(context.Context, *types.WebSearchProviderEntity) error {
	return nil
}
func (r *agentWebSearchRepo) Delete(context.Context, uint64, string) error       { return nil }
func (r *agentWebSearchRepo) ClearDefault(context.Context, uint64, string) error { return nil }
