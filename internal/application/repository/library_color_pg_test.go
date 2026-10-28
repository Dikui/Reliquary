package repository

import (
	"context"
	"fmt"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibraryColorConcurrentCreationUsesExistingCount(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Colors"}).Error)
	repo := NewLibraryRepository(db)
	ctx := context.Background()
	const count = 10
	libraries := make([]*types.Library, count)
	errors := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			library := &types.Library{
				SpaceID: 1, Name: fmt.Sprintf("Library %d", i), Directory: fmt.Sprintf("color-%d", i),
			}
			errors[i] = repo.CreateLibrary(ctx, library)
			libraries[i] = library
		}()
	}
	wg.Wait()
	colors := make([]int, count)
	for i, library := range libraries {
		require.NoError(t, errors[i])
		stored, err := repo.GetLibraryByID(ctx, library.ID)
		require.NoError(t, err)
		colors[i] = stored.Color
	}
	slices.Sort(colors)
	require.Equal(t, []int{1, 1, 2, 2, 3, 4, 5, 6, 7, 8}, colors)

	// A rejected insert must not consume a color in the next count.
	err := repo.CreateLibrary(ctx, &types.Library{SpaceID: 1, Name: "Duplicate", Directory: "color-0"})
	require.Error(t, err)
	next := &types.Library{SpaceID: 1, Name: "Next", Directory: "next"}
	require.NoError(t, repo.CreateLibrary(ctx, next))
	require.Equal(t, 3, next.Color)
	// Deleted libraries do not contribute to the current library count.
	require.NoError(t, db.Delete(libraries[0]).Error)
	require.NoError(t, db.Delete(libraries[1]).Error)
	next = &types.Library{SpaceID: 1, Name: "After deletion", Directory: "after-deletion"}
	require.NoError(t, repo.CreateLibrary(ctx, next))
	require.Equal(t, 2, next.Color)
}

func TestLibraryColorAllocationDoesNotRequireSpaceRow(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	library, err := EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	require.Equal(t, 1, library.Color)
}
