package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestUpdateEntryExplicitEmptyDescriptionClearsSummary(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateEntryRepo{entry: &types.Entry{
		ID:            "entry-1",
		SpaceID:       7,
		LibraryID:     "library-1",
		Description:   "old summary",
		SummaryStatus: types.SummaryStatusCompleted,
	}}
	service := &entryService{
		repo: repo, libraryService: metadataUpdateLibraryService{},
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	err := service.UpdateEntry(ctx, &types.Entry{
		ID:                   "entry-1",
		DescriptionSpecified: true,
		Description:          "",
	})
	require.NoError(t, err)
	require.Empty(t, repo.entry.Description)
	require.Equal(t, types.SummaryStatusNone, repo.entry.SummaryStatus)
}

func TestUpdateEntryOmittedDescriptionPreservesExisting(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateEntryRepo{entry: &types.Entry{
		ID:            "entry-1",
		SpaceID:       7,
		LibraryID:     "library-1",
		Description:   "keep me",
		SummaryStatus: types.SummaryStatusCompleted,
	}}
	service := &entryService{
		repo: repo, libraryService: metadataUpdateLibraryService{},
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	err := service.UpdateEntry(ctx, &types.Entry{
		ID:    "entry-1",
		Title: "new title",
	})
	require.NoError(t, err)
	require.Equal(t, "new title", repo.entry.Title)
	require.Equal(t, "keep me", repo.entry.Description)
	require.Equal(t, types.SummaryStatusCompleted, repo.entry.SummaryStatus)
}

func TestUpdateEntryManualDescriptionSetsCompletedStatus(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateEntryRepo{entry: &types.Entry{
		ID:            "entry-1",
		SpaceID:       7,
		LibraryID:     "library-1",
		SummaryStatus: types.SummaryStatusFailed,
	}}
	service := &entryService{
		repo: repo, libraryService: metadataUpdateLibraryService{},
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	err := service.UpdateEntry(ctx, &types.Entry{
		ID:                   "entry-1",
		DescriptionSpecified: true,
		Description:          "manual summary",
	})
	require.NoError(t, err)
	require.Equal(t, "manual summary", repo.entry.Description)
	require.Equal(t, types.SummaryStatusCompleted, repo.entry.SummaryStatus)
}

func TestUpdateEntryDescriptionOnlyDoesNotTouchMetadata(t *testing.T) {
	t.Parallel()
	repo := &metadataUpdateEntryRepo{entry: &types.Entry{
		ID:             "entry-1",
		SpaceID:        7,
		LibraryID:      "library-1",
		Description:    "old summary",
		SummaryStatus:  types.SummaryStatusCompleted,
		CustomMetadata: types.JSON(`{"region":"Beijing"}`),
	}}
	service := &entryService{
		repo: repo, libraryService: metadataUpdateLibraryService{},
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	err := service.UpdateEntry(ctx, &types.Entry{
		ID:                   "entry-1",
		DescriptionSpecified: true,
		Description:          "updated summary",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"region":"Beijing"}`, string(repo.entry.CustomMetadata))
}
