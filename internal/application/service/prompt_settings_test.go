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
	rows      map[string]*types.SystemSetting
	failWrite bool
	failList  bool
}

func (r *promptSettingsRepository) List(context.Context) ([]*types.SystemSetting, error) {
	if r.failList {
		return nil, errors.New("list temporarily unavailable")
	}
	rows := []*types.SystemSetting{}
	for _, row := range r.rows {
		rows = append(rows, row)
	}
	if r.row != nil {
		rows = append(rows, r.row)
	}
	return rows, nil
}

func (r *promptSettingsRepository) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	if row := r.rows[key]; row != nil {
		return row, nil
	}
	if r.row != nil && r.row.Key == key {
		return r.row, nil
	}
	return nil, nil
}

func (r *promptSettingsRepository) Upsert(_ context.Context, row *types.SystemSetting) error {
	if r.failWrite {
		return errors.New("storage unavailable")
	}
	if r.rows == nil {
		r.rows = map[string]*types.SystemSetting{}
	}
	r.rows[row.Key] = row
	return nil
}

func (r *promptSettingsRepository) Delete(_ context.Context, key string) (bool, error) {
	delete(r.rows, key)
	if r.row != nil && r.row.Key == key {
		r.row = nil
	}
	return true, nil
}

func TestPromptSettingsPersistAndApplyWithoutRestart(t *testing.T) {
	t.Cleanup(func() { config.SetPromptOverrides(nil) })
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "personal-user")
	repo := &promptSettingsRepository{}
	svc := NewSystemSettingService(repo, &config.Config{})
	libraryA, libraryB := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	key := promptSettingPrefix + "library." + libraryA + ".article_processing"
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
	require.Equal(t, len(config.PromptScopeIDs(false)), count)
	require.ElementsMatch(t, []string{"evaluation_questions_multi_library", "model_connection_test", "model_maintenance_probe"}, config.PromptScopeIDs(false))

	content := "保留换行与原文。\n只输出所需的 JSON。"
	row, err := svc.Update(ctx, key, content)
	require.NoError(t, err)
	require.False(t, row.RequiresRestart)
	require.Equal(t, content, config.Prompt("article_processing", libraryA))

	require.Equal(t, content, libraryProcessingPrompt(&types.Library{ID: libraryA}, &types.Entry{}))
	require.Equal(t, fallback, libraryProcessingPrompt(&types.Library{ID: libraryB}, &types.Entry{}))
	_, err = svc.Update(ctx, promptSettingPrefix+"library."+libraryB+".article_processing", "Independent library")
	require.NoError(t, err)
	require.Equal(t, content, config.Prompt("article_processing", libraryA))
	require.Equal(t, "Independent library", config.Prompt("article_processing", libraryB))
	config.SetPromptOverrides(nil)
	repo.failList = true
	svc = NewSystemSettingService(repo, &config.Config{})
	repo.failList = false
	_, err = svc.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, content, config.Prompt("article_processing", libraryA), "startup restores the saved prompt")
	require.Equal(t, fallback, config.PromptDefaults()["article_processing"])

	_, err = svc.Update(ctx, key, "  \n ")
	require.ErrorContains(t, err, "must not be empty")
	_, err = svc.Update(ctx, key, strings.Repeat("a", 64*1024+1))
	require.ErrorContains(t, err, "64 KiB")
	repo.failWrite = true
	_, err = svc.Update(ctx, key, "unsaved replacement")
	require.ErrorContains(t, err, "storage unavailable")
	require.Equal(t, content, config.Prompt("article_processing", libraryA), "failed writes never change the active prompt")

	require.NoError(t, svc.Reset(ctx, key))
	require.Equal(t, fallback, config.Prompt("article_processing", libraryA))
	require.Equal(t, "Independent library", config.Prompt("article_processing", libraryB))

	repo.row = &types.SystemSetting{Key: "prompt.library_media", Value: types.JSON(`"retired rule"`)}
	rows, err = svc.List(ctx)
	require.NoError(t, err)
	for _, row := range rows {
		require.NotEqual(t, "prompt.library_media", row.Key, "retired saved values must not appear in settings")
	}
	_, err = svc.Update(ctx, "prompt.article_processing", "Cannot edit an unscoped library prompt")
	require.ErrorContains(t, err, "unknown setting key")
	_, err = svc.Get(ctx, "prompt.library_media")
	require.ErrorContains(t, err, "unknown setting key")
}
