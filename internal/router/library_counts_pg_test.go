package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reliquary/internal/types"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

type libraryCountSQLRecorder struct {
	logger.Interface
	queries []string
}

func (r *libraryCountSQLRecorder) Trace(ctx context.Context, begin time.Time, sql func() (string, int64), err error) {
	query, rows := sql()
	if strings.Contains(query, `FROM "entries"`) {
		r.queries = append(r.queries, query)
	}
	r.Interface.Trace(ctx, begin, func() (string, int64) { return query, rows }, err)
}

func TestLibraryListCountsMatchDetails(t *testing.T) {
	f := newPersonalAuthFixture(t)
	var libraries []types.Library
	require.NoError(t, f.db.Order("created_at,id").Find(&libraries).Error)
	require.Len(t, libraries, 5)
	statuses := [][]string{
		{types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusCompleted},
		{types.ParseStatusCompleted, types.ParseStatusFailed},
		{types.ParseStatusRejected},
		nil,
		nil,
	}
	for i, library := range libraries {
		for j, status := range statuses[i] {
			entry := &types.Entry{
				ID: fmt.Sprintf("library-count-%d-%d", i, j), SpaceID: types.SingleSpaceID,
				LibraryID: library.ID, Type: "manual", SourceKind: "article", ParseStatus: status,
			}
			require.NoError(t, f.db.Create(entry).Error)
		}
	}
	// Deleted pending entries count neither toward the total nor toward processing.
	for _, i := range []int{0, 4} {
		entry := &types.Entry{
			ID: fmt.Sprintf("deleted-library-count-%d", i), SpaceID: types.SingleSpaceID,
			LibraryID: libraries[i].ID, Type: "manual", SourceKind: "article", ParseStatus: types.ParseStatusPending,
		}
		require.NoError(t, f.db.Create(entry).Error)
		require.NoError(t, f.db.Delete(entry).Error)
	}

	originalLogger := f.db.Logger
	recorder := &libraryCountSQLRecorder{Interface: originalLogger}
	f.db.Logger = recorder
	t.Cleanup(func() { f.db.Logger = originalLogger })
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/libraries", "", f.key, true)
	f.db.Logger = originalLogger
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var list struct {
		Success bool            `json:"success"`
		Data    []types.Library `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &list))
	require.True(t, list.Success)
	require.Len(t, list.Data, len(libraries))
	var total int64
	for i, library := range list.Data {
		require.Equal(t, libraries[i].ID, library.ID, "keep catalogue ordering")
		require.Equal(t, int64(len(statuses[i])), library.EntryCount, library.ID)
		expectedProcessing := int64(0)
		if i == 0 {
			expectedProcessing = 2
		}
		require.Equal(t, expectedProcessing, library.ProcessingCount, library.ID)
		require.Equal(t, expectedProcessing > 0, library.IsProcessing, library.ID)
		total += library.EntryCount

		detailResponse := personalRequest(t, f.router, http.MethodGet,
			"/api/v1/libraries/"+library.ID, "", f.key, true)
		require.Equal(t, http.StatusOK, detailResponse.Code, detailResponse.Body.String())
		var detail struct {
			Data types.Library `json:"data"`
		}
		require.NoError(t, json.Unmarshal(detailResponse.Body.Bytes(), &detail))
		require.Equal(t, detail.Data.EntryCount, library.EntryCount, library.ID)
		require.Equal(t, detail.Data.ProcessingCount, library.ProcessingCount, library.ID)
		require.Equal(t, detail.Data.IsProcessing, library.IsProcessing, library.ID)
	}
	require.Equal(t, int64(6), total)
	require.Len(t, recorder.queries, 1, "list counts must use one query, not queries per library")
	require.Contains(t, strings.ToLower(recorder.queries[0]), "group by")
}
