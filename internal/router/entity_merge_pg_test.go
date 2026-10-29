package router

import (
	"encoding/json"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManualEntityMergeClosesObsoleteSuggestionsAtomically(t *testing.T) {
	for _, failLog := range []bool{false, true} {
		name := "successful merge"
		if failLog {
			name = "failed merge rolls back suggestions"
		}
		t.Run(name, func(t *testing.T) {
			db := entityFixtureDB(t)
			require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Space"}).Error)
			for _, id := range []string{"library", "other-library"} {
				library := types.Library{ID: id, SpaceID: 1, Name: id, Directory: id}
				library.EnsureDefaults()
				require.NoError(t, db.Create(&library).Error)
			}
			for _, id := range []string{"keep", "merged", "third", "fourth", "other-a", "other-b"} {
				libraryID := "library"
				if id == "other-a" || id == "other-b" {
					libraryID = "other-library"
				}
				require.NoError(t, db.Create(&types.Entity{
					ID: id, SpaceID: 1, LibraryID: libraryID,
					CanonicalName: id, NormalizedName: id, EntityType: "company",
				}).Error)
			}
			suggestions := []types.EntityMergeSuggestion{
				{ID: "pair", NewEntityID: "merged", CandidateEntityID: "keep", Status: "pending"},
				{ID: "reverse-pair", NewEntityID: "keep", CandidateEntityID: "merged", Status: "pending"},
				{ID: "obsolete-new", NewEntityID: "merged", CandidateEntityID: "third", Status: "pending"},
				{ID: "obsolete-candidate", NewEntityID: "third", CandidateEntityID: "merged", Status: "pending"},
				{ID: "keep-third", NewEntityID: "keep", CandidateEntityID: "third", Status: "pending"},
				{ID: "unrelated", NewEntityID: "third", CandidateEntityID: "fourth", Status: "pending"},
				{ID: "other-library", NewEntityID: "other-a", CandidateEntityID: "other-b", Status: "pending"},
				{ID: "already-rejected", NewEntityID: "merged", CandidateEntityID: "fourth", Status: "rejected"},
			}
			for i := range suggestions {
				suggestions[i].SpaceID = 1
				suggestions[i].EntryID = suggestions[i].ID
			}
			require.NoError(t, db.Create(&suggestions).Error)
			if failLog {
				require.NoError(t, db.Exec(`CREATE FUNCTION reject_merge_log() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'injected merge log failure'; END $$;
CREATE TRIGGER reject_merge_log BEFORE INSERT ON operation_logs
FOR EACH ROW EXECUTE FUNCTION reject_merge_log();`).Error)
			}
			engine := gin.New()
			engine.Use(middleware.ErrorHandler(), entityIdentityMiddleware())
			RegisterEntityRoutes(engine.Group("/api/v1"), handler.NewEntityHandler(service.NewEntityService(db, nil)))
			response := entityRequest(t, engine, http.MethodPost, "/api/v1/entities/merge",
				`{"keep_entity_id":"keep","merge_entity_id":"merged"}`, 1, entityOwnerID)
			if failLog {
				require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
			} else {
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			}
			for _, libraryID := range []string{"library", "other-library"} {
				response = entityRequest(t, engine, http.MethodGet,
					"/api/v1/entities/merge-suggestions?status=all&library_id="+libraryID, "", 1, entityOwnerID)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var envelope struct{ Data []types.EntityMergeSuggestion }
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
				expectedCount := 7
				if libraryID == "other-library" {
					expectedCount = 1
				}
				require.Len(t, envelope.Data, expectedCount)
				for _, suggestion := range envelope.Data {
					want := "pending"
					if suggestion.ID == "already-rejected" {
						want = "rejected"
					} else if !failLog {
						switch suggestion.ID {
						case "pair", "reverse-pair":
							want = "accepted"
						case "obsolete-new", "obsolete-candidate":
							want = "rejected"
						}
					}
					require.Equal(t, want, suggestion.Status, suggestion.ID)
					if !failLog && (want == "accepted" || suggestion.ID == "obsolete-new" ||
						suggestion.ID == "obsolete-candidate") {
						require.NotNil(t, suggestion.DecidedAt, suggestion.ID)
					} else {
						require.Nil(t, suggestion.DecidedAt, suggestion.ID)
					}
				}
				response = entityRequest(t, engine, http.MethodGet,
					"/api/v1/entities/merge-suggestions?status=pending&library_id="+libraryID, "", 1, entityOwnerID)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
				pendingCount := 1
				if libraryID == "library" {
					pendingCount = 2
					if failLog {
						pendingCount = 6
					}
				}
				require.Len(t, envelope.Data, pendingCount)
			}
		})
	}
}
