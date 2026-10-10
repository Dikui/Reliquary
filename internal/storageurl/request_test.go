package storageurl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func publicRewriter(url string) *Rewriter {
	return NewRewriter(stubResolver(url), "TEST")
}

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

// Public references must not mutate the original retrieval result.
func TestCopyReferences_DoesNotMutateOriginals(t *testing.T) {
	w := publicRewriter("https://cdn.example.com/x.png")
	original := &types.SearchResult{
		Content:        "chunk ![c](resource://xifDo7NTSL300Lp1goVutw)",
		MatchedContent: "match ![m](resource://xifDo7NTSL300Lp1goVutw)",
		ImageInfo:      `[{"url":"resource://xifDo7NTSL300Lp1goVutw"}]`,
	}
	refs := []*types.SearchResult{original, nil}

	out := w.CopyReferences(context.Background(), refs)

	require.Len(t, out, 2)
	assert.NotSame(t, original, out[0])
	assert.Equal(t, "chunk ![c](resource://xifDo7NTSL300Lp1goVutw)", original.Content,
		"the source retrieval result must be untouched")
	assert.Equal(t, "chunk ![c](https://cdn.example.com/x.png)", out[0].Content)
	assert.Equal(t, "match ![m](https://cdn.example.com/x.png)", out[0].MatchedContent)
	assert.Equal(t, `[{"url":"https://cdn.example.com/x.png"}]`, out[0].ImageInfo)
	assert.Nil(t, out[1])
}

func TestCopyReferences_DisabledReturnsInput(t *testing.T) {
	w := NewRewriter(nil, "TEST")
	refs := []*types.SearchResult{{Content: "![a](resource://xifDo7NTSL300Lp1goVutw)"}}
	assert.Equal(t, refs, w.CopyReferences(context.Background(), refs))
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
