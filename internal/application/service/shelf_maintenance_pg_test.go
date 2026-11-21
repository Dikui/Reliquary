package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestShelfWriteTransactionRejectsARequestAdmittedBeforePause(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(t.Context(), types.SpaceIDContextKey, uint64(types.SingleSpaceID))
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "editor"})
	require.NoError(t, EnsurePersonalSpace(ctx, db))
	libraries := NewLibraryService(db, repository.NewLibraryRepository(db), nil, nil, nil, nil, nil)
	library, err := libraries.CreateLibrary(ctx, types.CreateLibraryRequest{
		Name: "Synthetic shelf", Directory: "synthetic-shelf", ShelfEnabled: true, EntityMode: types.EntityModeManaged,
	})
	require.NoError(t, err)
	entities := NewEntityService(db, nil)
	input := types.ShelfChangesetInput{
		Source: types.OperationSource{Type: "web"},
		Operations: []types.ShelfOperation{{ID: "line", Type: "create_entity", Entity: &types.ManagedEntityInput{
			Name: "Synthetic line", Kind: "product_line",
		}}},
	}
	draft, err := entities.SubmitShelfChangeset(ctx, library.ID, input)
	require.NoError(t, err)
	review := types.ShelfChangesetReview{Revision: draft.Revision, Decisions: []types.ShelfDecision{{ItemID: "line", Action: "apply"}}}

	// The request passed its transport check. Pause wins before its write transaction starts.
	require.NoError(t, maintenanceops.CheckBusinessWrite(ctx, db))
	require.NoError(t, maintenanceops.Pause(ctx, db, "drain", "pack", nil))
	_, err = entities.ReviewShelfChangeset(ctx, library.ID, draft.ID, review, true)
	var app *apperrors.AppError
	require.ErrorAs(t, err, &app)
	require.Equal(t, http.StatusServiceUnavailable, app.HTTPCode)
	require.Contains(t, app.Message, "原因：pack")
	saved, err := entities.ShelfChangeset(ctx, library.ID, draft.ID)
	require.NoError(t, err)
	require.Equal(t, draft.Revision, saved.Revision)
	require.Equal(t, "pending", saved.Status)
	require.NoError(t, maintenanceops.Resume(ctx, db))
	saved, err = entities.ReviewShelfChangeset(ctx, library.ID, draft.ID, review, true)
	require.NoError(t, err)
	require.Equal(t, "confirmed", saved.Status)
}
