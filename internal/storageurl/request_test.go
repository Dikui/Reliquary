package storageurl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRequestRewriter_HandleModeIsDisabled(t *testing.T) {
	w := NewRequestRewriter(context.Background(), ModeHandle, &stubFileService{})
	assert.False(t, w.Enabled(), "the default mode must not resolve anything")
}

func TestNewRequestRewriter_PublicModeIsEnabled(t *testing.T) {
	w := NewRequestRewriter(context.Background(), ModePublic, &stubFileService{})
	require.True(t, w.Enabled())
	assert.Equal(t, "https://cdn.example.com/resource://xifDo7NTSL300Lp1goVutw",
		w.Ref(context.Background(), "resource://xifDo7NTSL300Lp1goVutw"))
}

func TestDefaultMode(t *testing.T) {
	ctx := context.Background()
	t.Setenv(EnvVar, "")
	assert.Equal(t, ModeHandle, DefaultMode(ctx))

	t.Setenv(EnvVar, "public")
	assert.Equal(t, ModePublic, DefaultMode(ctx))

	t.Setenv(EnvVar, "nonsense")
	assert.Equal(t, ModeHandle, DefaultMode(ctx), "a typo must degrade to the safe default")
}

func TestResolveMode_QueryWinsOverDeployment(t *testing.T) {
	ctx := context.Background()
	t.Setenv(EnvVar, "public")

	mode, err := ResolveMode(ctx, "handle")
	require.NoError(t, err)
	assert.Equal(t, ModeHandle, mode, "an explicit query value must win")

	mode, err = ResolveMode(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, ModePublic, mode)

	_, err = ResolveMode(ctx, "yes-please")
	assert.Error(t, err, "an invalid query value is a client error")
}
