package sandbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

func newTenantKeyTestManager(t *testing.T) (*SessionBoundManager, *fakeRemoteClient) {
	t.Helper()

	client := newFakeRemoteClient(SandboxTypeDocker)
	cfg := DefaultConfig()
	cfg.DockerImage = "tpl-test"
	mgr, err := NewSessionBoundManager(SessionBoundManagerConfig{
		Config:          cfg,
		Client:          client,
		Store:           NewMemorySessionSandboxBindingStore(),
		Checker:         &fakeSessionExistenceChecker{exists: true},
		SkipHealthProbe: true,
	})
	require.NoError(t, err)
	return mgr, client
}

// A team session must release the Docker container created for its execution.
func TestSessionBindingSurvivesTeamContextAcrossDestroy(t *testing.T) {
	const sessionOwner = uint64(1)

	mgr, client := newTenantKeyTestManager(t)

	// Execute under the team tenant.
	execCtx := context.WithValue(context.Background(), types.TenantIDContextKey, sessionOwner)
	execCtx = types.WithSandboxTenantID(execCtx, sessionOwner)
	_, err := mgr.Execute(execCtx, &ExecuteConfig{
		SessionID:      "session-team",
		SkipValidation: true,
		ScriptContent:  "print('ok')\n",
		Script:         "hello.py",
	})
	require.NoError(t, err)

	client.mu.Lock()
	created := client.createCount
	client.mu.Unlock()
	require.Equal(t, 1, created, "execution should have provisioned one sandbox")

	// Teardown: only the session's own tenant is known here, and no sandbox
	// tenant key is set — exactly what sessionService.destroyBoundSandbox does.
	destroyCtx := context.WithValue(context.Background(), types.TenantIDContextKey, sessionOwner)
	require.NoError(t, mgr.DestroySession(destroyCtx, "session-team"))

	client.mu.Lock()
	deleted := append([]string(nil), client.deleteIDs...)
	client.mu.Unlock()
	require.Len(t, deleted, 1, "teardown must reach the sandbox execution created")
}

func TestSessionBindingRejectsOtherTeam(t *testing.T) {
	mgr, client := newTenantKeyTestManager(t)
	for _, ctx := range []context.Context{
		context.WithValue(context.Background(), types.TenantIDContextKey, uint64(2)),
		types.WithSandboxTenantID(context.Background(), 2),
	} {
		_, err := mgr.Execute(ctx, &ExecuteConfig{
			SessionID: "session-other-team", SkipValidation: true,
			ScriptContent: "print('ok')\n", Script: "hello.py",
		})
		require.Error(t, err)
	}
	client.mu.Lock()
	created := client.createCount
	client.mu.Unlock()
	require.Zero(t, created)
}
