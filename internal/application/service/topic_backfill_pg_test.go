package service

import (
	"context"
	"encoding/json"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"testing"

	"github.com/stretchr/testify/require"
	taskqueue "reliquary/internal/queue"
)

func TestTopicBackfillUsesOwningLibraryModel(t *testing.T) {
	for _, test := range []struct {
		name           string
		officialExists bool
		modelID        string
	}{
		{name: "configured without official library", modelID: "custom-model"},
		{name: "configured with official library", officialExists: true, modelID: "custom-model"},
		{name: "unconfigured without official library"},
		{name: "unconfigured with official library", officialExists: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := pgtest.Open(t)
			ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "owner"})
			ctx = context.WithValue(ctx, types.SpaceIDContextKey, types.SingleSpaceID)
			require.NoError(t, db.Create(&types.Space{ID: types.SingleSpaceID, Name: "Space"}).Error)
			library := types.Library{
				ID: "custom", SpaceID: types.SingleSpaceID, Name: "Custom",
				ModelTiers: types.ModelTierConfig{Generation: test.modelID},
			}
			library.EnsureDefaults()
			require.NoError(t, db.Create(&library).Error)
			if test.officialExists {
				official := types.SystemLibraryDefaults()[0]
				official.ID, official.SpaceID = "official", types.SingleSpaceID
				official.ModelTiers.Generation = "official-model"
				require.NoError(t, db.Create(official).Error)
			}
			require.NoError(t, db.Create(&types.Entry{
				ID: "historical", SpaceID: types.SingleSpaceID,
				LibraryID: library.ID, Type: types.EntryTypeManual, SourceKind: "article", Title: "Historical",
				Description: "Research findings", ParseStatus: types.ParseStatusCompleted,
			}).Error)
			model := &topicJudgementChat{}
			models := &tierTestModels{chat: model}
			svc := NewTopicService(db)
			svc.SetModelService(models)
			topic, err := svc.CreateTopic(ctx, &types.Topic{LibraryID: library.ID, Name: "Research"})
			require.NoError(t, err)
			job, err := svc.BackfillStatus(ctx, topic.ID)
			require.NoError(t, err)
			payload, err := json.Marshal(types.TopicBackfillPayload{SpaceID: types.SingleSpaceID, JobID: job.ID})
			require.NoError(t, err)
			require.NoError(t, svc.HandleBackfill(ctx, taskqueue.NewTask(types.TypeTopicBackfill, payload)))
			job, err = svc.BackfillStatus(ctx, topic.ID)
			require.NoError(t, err)
			if test.modelID == "" {
				require.Equal(t, "failed", job.Status)
				require.Equal(t, 1, job.FailedEntries)
				require.Len(t, job.Batches, 1)
				require.Equal(t, "generation model is not configured", job.Batches[0].Error)
				require.Empty(t, models.selected)
				require.Empty(t, model.calls())
				return
			}
			require.Equal(t, "completed", job.Status)
			require.Equal(t, 1, job.Processed)
			require.Equal(t, test.modelID, models.selected)
			topics, err := svc.ListTopics(ctx, library.ID)
			require.NoError(t, err)
			require.Len(t, topics, 1)
			require.EqualValues(t, 1, topics[0].EntryCount)
		})
	}
}
