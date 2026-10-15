package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type reparseFailureEntryRepo struct {
	interfaces.EntryRepository
	entry       *types.Entry
	updateCalls int
}

func (r *reparseFailureEntryRepo) GetEntryByID(
	_ context.Context,
	_ uint64,
	_ string,
) (*types.Entry, error) {
	return r.entry, nil
}

func (r *reparseFailureEntryRepo) UpdateEntry(
	_ context.Context,
	_ *types.Entry,
) error {
	r.updateCalls++
	return nil
}

func (r *reparseFailureEntryRepo) UpdateEntryColumn(
	_ context.Context,
	_ string,
	_ string,
	_ interface{},
) error {
	return nil
}

type reparseFailureLibraryService struct {
	interfaces.LibraryService
	library *types.Library
}

func (s *reparseFailureLibraryService) GetLibraryByID(
	_ context.Context,
	_ string,
) (*types.Library, error) {
	return s.library, nil
}

type failingReparseTaskEnqueuer struct {
	err error
}

func (e failingReparseTaskEnqueuer) Enqueue(
	_ *taskqueue.Task,
	_ ...taskqueue.Option,
) (*taskqueue.TaskInfo, error) {
	return nil, e.err
}

func TestReparseEntryManualEnqueueFailureIsVisible(t *testing.T) {
	enqueueErr := errors.New("queue unavailable")
	entry := &types.Entry{
		ID:           "entry-1",
		SpaceID:      7,
		LibraryID:    "library-1",
		Type:         types.EntryTypeManual,
		ParseStatus:  types.ParseStatusCompleted,
		EnableStatus: "enabled",
	}
	require.NoError(t, entry.SetManualMetadata(
		types.NewManualEntryMetadata("# content", types.ManualEntryStatusPublish, 1),
	))
	repo := &reparseFailureEntryRepo{entry: entry}
	svc := &entryService{
		repo:           repo,
		libraryService: &reparseFailureLibraryService{library: &types.Library{ID: "library-1"}},
		task:           failingReparseTaskEnqueuer{err: enqueueErr},
	}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(7))

	got, err := svc.ReparseEntry(ctx, entry.ID, nil)

	require.Error(t, err)
	require.Same(t, entry, got)
	require.Equal(t, types.ParseStatusFailed, entry.ParseStatus)
	require.Equal(t, "disabled", entry.EnableStatus)
	require.Equal(t, "Failed to enqueue processing task", entry.ErrorMessage)
	require.GreaterOrEqual(t, repo.updateCalls, 2, "pending and failed states must both be persisted")
}

func TestRunEntryListReparseSubmissionsReportsPartialFailure(t *testing.T) {
	firstErr := errors.New("first failed")
	secondErr := errors.New("second failed")
	var attempted []string

	outcome, err := runEntryListReparseSubmissions(
		[]string{"ok-1", "bad-1", "ok-2", "bad-2"},
		func(id string) error {
			attempted = append(attempted, id)
			switch id {
			case "bad-1":
				return firstErr
			case "bad-2":
				return secondErr
			default:
				return nil
			}
		},
	)

	require.Equal(t, []string{"ok-1", "bad-1", "ok-2", "bad-2"}, attempted)
	require.Equal(t, entryListReparseOutcome{Submitted: 2, Failed: 2}, outcome)
	require.ErrorIs(t, err, taskqueue.SkipRetry)
	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.ErrorContains(t, err, "entry bad-1")
	require.ErrorContains(t, err, "entry bad-2")
}

func TestRunEntryListReparseSubmissionsSucceeds(t *testing.T) {
	outcome, err := runEntryListReparseSubmissions(
		[]string{"entry-1", "entry-2"},
		func(string) error { return nil },
	)

	require.NoError(t, err)
	require.Equal(t, entryListReparseOutcome{Submitted: 2}, outcome)
}
