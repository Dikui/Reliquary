package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type promptSettingsRepository struct {
	interfaces.SystemSettingRepository
	row       *types.SystemSetting
	failWrite bool
	failList  bool
}

func (r *promptSettingsRepository) List(context.Context) ([]*types.SystemSetting, error) {
	if r.failList {
		return nil, errors.New("list temporarily unavailable")
	}
	if r.row == nil {
		return nil, nil
	}
	return []*types.SystemSetting{r.row}, nil
}

func (r *promptSettingsRepository) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	if r.row != nil && r.row.Key == key {
		return r.row, nil
	}
	return nil, nil
}

func (r *promptSettingsRepository) Upsert(_ context.Context, row *types.SystemSetting) error {
	if r.failWrite {
		return errors.New("storage unavailable")
	}
	r.row = row
	return nil
}

func (r *promptSettingsRepository) Delete(context.Context, string) (bool, error) {
	r.row = nil
	return true, nil
}

func TestPromptSettingsPersistAndApplyWithoutRestart(t *testing.T) {
	t.Cleanup(func() { config.SetPromptOverrides(nil) })
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "personal-user")
	repo := &promptSettingsRepository{}
	svc := NewSystemSettingService(repo, &config.Config{})
	key := promptSettingPrefix + "article_processing"
	fallback := config.PromptDefaults()["article_processing"]
	require.NotEmpty(t, fallback)

	rows, err := svc.List(ctx)
	require.NoError(t, err)
	count := 0
	for _, row := range rows {
		if row.Category == "prompts" {
			count++
			value, err := row.AsString()
			require.NoError(t, err)
			require.NotEmpty(t, value)
		}
	}
	require.Equal(t, len(config.PromptIDs()), count)
	require.ElementsMatch(t, []string{"article_processing", "summary_merge", "evaluation_questions"}, config.PromptIDs())

	content := "保留换行与原文。\n只输出所需的 JSON。"
	row, err := svc.Update(ctx, key, content)
	require.NoError(t, err)
	require.False(t, row.RequiresRestart)
	require.Equal(t, content, config.Prompt("article_processing"))

	config.SetPromptOverrides(nil)
	repo.failList = true
	svc = NewSystemSettingService(repo, &config.Config{})
	repo.failList = false
	require.Equal(t, content, config.Prompt("article_processing"), "startup restores the saved prompt")
	require.Equal(t, fallback, config.PromptDefaults()["article_processing"])

	_, err = svc.Update(ctx, key, "  \n ")
	require.ErrorContains(t, err, "must not be empty")
	_, err = svc.Update(ctx, key, strings.Repeat("a", 64*1024+1))
	require.ErrorContains(t, err, "64 KiB")
	repo.failWrite = true
	_, err = svc.Update(ctx, key, "unsaved replacement")
	require.ErrorContains(t, err, "storage unavailable")
	require.Equal(t, content, config.Prompt("article_processing"), "failed writes never change the active prompt")

	require.NoError(t, svc.Reset(ctx, key))
	require.Equal(t, fallback, config.Prompt("article_processing"))

	repo.row = &types.SystemSetting{Key: "prompt.library_media", Value: types.JSON(`"retired rule"`)}
	rows, err = svc.List(ctx)
	require.NoError(t, err)
	for _, row := range rows {
		require.NotEqual(t, "prompt.library_media", row.Key, "retired saved values must not appear in settings")
	}
	_, err = svc.Get(ctx, "prompt.library_media")
	require.ErrorContains(t, err, "unknown setting key")
}
