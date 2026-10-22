package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

func graphFixture(t *testing.T) (*gorm.DB, context.Context, *EntityService, *entryService) {
	db, ctx, entities := entityFixture(t)
	for _, kind := range []string{"合作", "投资", "持有"} {
		require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "entity-library", Name: kind, Enabled: true}).Error)
	}
	for _, kind := range []string{"on", "off"} {
		require.NoError(t, db.Create(&types.LibraryContentType{LibraryID: "entity-library", Name: kind, Enabled: true, ExtractGraph: kind == "on"}).Error)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		require.NoError(t, db.Create(ptrEntity(entityRecord(id, id, "company"))).Error)
	}
	svc := &entryService{db: db, repo: repository.NewEntryRepository(db), libraryService: NewLibraryService(db, repository.NewLibraryRepository(db), repository.NewEntryRepository(db), repository.NewChunkRepository(db), nil, nil, nil)}
	return db, ctx, entities, svc
}
func ptrEntity(e types.Entity) *types.Entity { return &e }
func graphEntry(t *testing.T, db *gorm.DB, id, ref string, at time.Time) *types.Entry {
	entry := entityArticle(t, db, id)
	entry.SourceKind = "mine"
	entry.ContentType = "on"
	entry.SourceRef = ref
	entry.CreatedAt = at
	entry.VaultBody = "# " + id + "\n\n完整保存的资料正文。"
	require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "approved"}))
	require.NoError(t, db.Save(entry).Error)
	return entry
}
func graphInput(a, kind, b string) types.ProcessingRelation {
	return types.ProcessingRelation{SubjectID: a, Type: kind, ObjectID: b, Evidence: []types.RelationEvidence{{Locations: []types.SourceLocation{{Start: 0, End: 1}}, Speaker: &types.ProcessingSpeaker{EntityID: "c", Name: "C", Selection: "extracted_person"}}}}
}
func graphPublish(t *testing.T, db *gorm.DB, ctx context.Context, svc *entryService, entry *types.Entry, relations ...types.ProcessingRelation) {
	t.Helper()
	out := types.EntryProcessingOutput{EntryID: entry.ID, SpaceID: entry.SpaceID, LibraryID: entry.LibraryID, RunID: "run-" + entry.ID, BodyHash: fmt.Sprintf("%x", sha256.Sum256([]byte(entry.VaultBody))), ContentType: entry.ContentType, Relations: relations, Images: []types.ProcessingImage{}}
	if out.Relations == nil {
		out.Relations = []types.ProcessingRelation{}
	}
	require.NoError(t, db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&out).Error)
	require.NoError(t, svc.publishEntryRelations(ctx, entry.ID, out.RunID))
}
func graphRows(t *testing.T, db *gorm.DB) []types.Relation {
	t.Helper()
	rows := []types.Relation{}
	require.NoError(t, db.Order("id").Find(&rows).Error)
	return rows
}
func graphPending(t *testing.T, db *gorm.DB) []types.GraphPendingSync {
	t.Helper()
	rows := []types.GraphPendingSync{}
	require.NoError(t, db.Order("id").Find(&rows).Error)
	return rows
}
func graphSources(t *testing.T, db *gorm.DB, id string) []types.RelationSource {
	t.Helper()
	rows := []types.RelationSource{}
	require.NoError(t, db.Where("relation_id=?", id).Order("entry_id").Find(&rows).Error)
	return rows
}
func TestIKB12SourcesVersionsSuppressionAndLibraryIsolation(t *testing.T) {
	t.Parallel()
	db, ctx, entities, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	two := graphEntry(t, db, "two", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"))
	first := graphRows(t, db)[0]
	require.EqualValues(t, 1, first.Version)
	require.Len(t, graphPending(t, db), 2)
	graphPublish(t, db, ctx, svc, two, graphInput("a", "合作", "b"))
	second := graphRows(t, db)[0]
	require.Equal(t, first.ID, second.ID)
	require.EqualValues(t, 2, second.Version)
	require.Len(t, graphSources(t, db, second.ID), 2)
	groups, err := entities.ListRelations(ctx, "a", "")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, 2, groups[0].Relations[0].SourceCount)
	require.Len(t, groups[0].Relations[0].Sources[0].Evidence, 1)
	require.NoError(t, svc.repo.DeleteEntry(ctx, 1, one.ID))
	third := graphRows(t, db)[0]
	require.EqualValues(t, 3, third.Version)
	require.Len(t, graphSources(t, db, third.ID), 1)
	require.NoError(t, svc.repo.DeleteEntry(ctx, 1, two.ID))
	require.Empty(t, graphRows(t, db))
	require.Len(t, graphPending(t, db), 8)
	for _, pending := range graphPending(t, db) {
		if pending.Deleted {
			require.Equal(t, first.ID, pending.RelationID)
			require.EqualValues(t, 4, pending.RelationVersion)
		}
	}
	three := graphEntry(t, db, "three", "", time.Now())
	graphPublish(t, db, ctx, svc, three, graphInput("a", "合作", "b"))
	row := graphRows(t, db)[0]
	require.NoError(t, entities.DeleteRelation(ctx, row.ID))
	graphPublish(t, db, ctx, svc, three, graphInput("a", "合作", "b"))
	require.Empty(t, graphRows(t, db))
	var logs int64
	require.NoError(t, db.Model(&types.OperationLog{}).Where("operation_type='delete_relation'").Count(&logs).Error)
	require.EqualValues(t, 1, logs)
	other := types.Library{ID: "other-library", SpaceID: 1, Name: "Other", Directory: "other-library", Template: types.TemplateExternalTimely, SourceEvidenceRule: "link_required"}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: other.ID, Name: "合作", Enabled: true}).Error)
	require.NoError(t, db.Create(&types.LibraryContentType{LibraryID: other.ID, Name: "on", Enabled: true, ExtractGraph: true}).Error)
	four := &types.Entry{ID: "four", SpaceID: 1, LibraryID: other.ID, ContentType: "on", ParseStatus: types.ParseStatusCompleted, Type: types.EntryTypeManual}
	require.NoError(t, db.Create(four).Error)
	graphPublish(t, db, ctx, svc, four, graphInput("a", "合作", "b"))
	require.Len(t, graphRows(t, db), 1)
	require.Equal(t, other.ID, graphRows(t, db)[0].LibraryID)
}
func TestIKB12RelationTypeMergeAndOldNameMapping(t *testing.T) {
	t.Parallel()
	db, ctx, entities, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	two := graphEntry(t, db, "two", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"), graphInput("a", "投资", "b"))
	graphPublish(t, db, ctx, svc, two, graphInput("a", "投资", "b"))
	before := len(graphPending(t, db))
	require.NoError(t, entities.MergeRelationTypes(ctx, "entity-library", "合作", "投资"))
	rows := graphRows(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, "投资", rows[0].RelationType)
	require.GreaterOrEqual(t, rows[0].Version, int64(3))
	sources := graphSources(t, db, rows[0].ID)
	require.Len(t, sources, 2)
	for _, source := range sources {
		if source.EntryID == one.ID {
			require.Len(t, source.Evidence, 2)
		}
	}
	require.Greater(t, len(graphPending(t, db)), before)
	var library types.Library
	require.NoError(t, db.First(&library, "id='entity-library'").Error)
	mapped, err := svc.canonicalRelationTypes(ctx, &library, []string{"合作"})
	require.NoError(t, err)
	require.Equal(t, "投资", mapped["合作"])
	graphPublish(t, db, ctx, svc, two, graphInput("a", "合作", "b"))
	require.Equal(t, "投资", graphRows(t, db)[0].RelationType)
	require.NoError(t, entities.MergeRelationTypes(ctx, "entity-library", "投资", "持有"))
	graphPublish(t, db, ctx, svc, two, graphInput("a", "合作", "b"))
	require.Equal(t, "持有", graphRows(t, db)[0].RelationType)
	require.NoError(t, entities.MergeRelationTypes(ctx, "entity-library", "持有", "合作"))
	require.NoError(t, entities.DeleteRelation(ctx, graphRows(t, db)[0].ID))
	graphPublish(t, db, ctx, svc, two, graphInput("a", "合作", "b"))
	require.Empty(t, graphRows(t, db))
}
func TestIKB12EntityMergeBothEndsCollisionSelfSuppressionAndSpeakers(t *testing.T) {
	t.Parallel()
	db, ctx, entities, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	two := graphEntry(t, db, "two", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("c", "合作", "b"), graphInput("a", "投资", "c"), graphInput("a", "持有", "c"), graphInput("c", "持有", "d"))
	graphPublish(t, db, ctx, svc, two, graphInput("a", "合作", "b"), graphInput("d", "投资", "c"), graphInput("a", "持有", "d"))
	for _, row := range graphRows(t, db) {
		if row.SubjectID == "c" && row.ObjectID == "d" {
			require.NoError(t, entities.DeleteRelation(ctx, row.ID))
		}
	}
	before := len(graphPending(t, db))
	require.NoError(t, entities.MergeEntities(ctx, "a", "c"))
	require.Greater(t, len(graphPending(t, db)), before)
	rows := graphRows(t, db)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.NotEqual(t, "c", row.SubjectID)
		require.NotEqual(t, "c", row.ObjectID)
		require.NotEqual(t, row.SubjectID, row.ObjectID)
		for _, source := range graphSources(t, db, row.ID) {
			for _, e := range source.Evidence {
				require.Equal(t, "a", e.Speaker.EntityID)
			}
		}
		if row.RelationType == "合作" {
			require.Len(t, graphSources(t, db, row.ID), 2)
		}
	}
	var suppressions []types.RelationSuppression
	require.NoError(t, db.Find(&suppressions).Error)
	require.Len(t, suppressions, 1)
	require.Equal(t, "a", suppressions[0].SubjectID)
	graphPublish(t, db, ctx, svc, one, graphInput("c", "持有", "d"))
	for _, row := range graphRows(t, db) {
		require.NotEqual(t, "持有", row.RelationType)
	}
}
func TestIKB12SourceRefNewestAndDeletionFallback(t *testing.T) {
	t.Parallel()
	db, ctx, _, svc := graphFixture(t)
	now := time.Now().Add(-time.Hour)
	old := graphEntry(t, db, "old", "source", now)
	graphPublish(t, db, ctx, svc, old, graphInput("a", "合作", "b"))
	latest := graphEntry(t, db, "latest", "source", now.Add(time.Minute))
	require.Len(t, graphRows(t, db), 1)
	graphPublish(t, db, ctx, svc, latest, graphInput("d", "投资", "b"))
	rows := graphRows(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, "d", rows[0].SubjectID)
	graphPublish(t, db, ctx, svc, old, graphInput("a", "合作", "b"))
	require.Equal(t, "d", graphRows(t, db)[0].SubjectID)
	before := len(graphPending(t, db))
	require.NoError(t, svc.repo.DeleteEntry(ctx, 1, latest.ID))
	require.Greater(t, len(graphPending(t, db)), before)
	require.Equal(t, "a", graphRows(t, db)[0].SubjectID)
	latest = graphEntry(t, db, "latest2", "source", now.Add(2*time.Minute))
	graphPublish(t, db, ctx, svc, latest, graphInput("d", "投资", "b"))
	require.NoError(t, svc.repo.DeleteEntry(ctx, 1, old.ID))
	require.Equal(t, "d", graphRows(t, db)[0].SubjectID)
	require.NoError(t, svc.repo.DeleteEntry(ctx, 1, latest.ID))
	require.Empty(t, graphRows(t, db))
}
func graphSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	data := map[string]any{}
	for _, table := range []string{"relations", "relation_sources", "relation_suppressions", "relation_type_merges", "graph_pending_sync", "operation_logs", "entries", "entities", "task_pending_ops", "library_relation_types"} {
		var raw string
		require.NoError(t, db.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM "+table+" t").Scan(&raw).Error)
		data[table] = raw
	}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	return string(raw)
}
func TestIKB12EveryMutationRollsBackAuthorityAndPendingTogether(t *testing.T) {
	t.Parallel()
	mutations := []string{"replace", "withdraw", "delete_entry", "delete_relation", "merge_entity", "merge_type", "fallback"}
	for _, name := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, ctx, entities, svc := graphFixture(t)
			one := graphEntry(t, db, "one", "source", time.Now().Add(-time.Hour))
			graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"))
			two := graphEntry(t, db, "two", "source", time.Now())
			if name == "fallback" {
				graphPublish(t, db, ctx, svc, two, graphInput("d", "投资", "b"))
			}
			require.NoError(t, db.Exec(`CREATE FUNCTION reject_ikb12_pending() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'ikb12 pending unavailable'; END $$; CREATE TRIGGER reject_ikb12_pending BEFORE INSERT ON graph_pending_sync FOR EACH ROW EXECUTE FUNCTION reject_ikb12_pending()`).Error)
			before := graphSnapshot(t, db)
			var err error
			switch name {
			case "replace":
				err = svc.publishEntryRelations(ctx, one.ID, "run-one") // Latest has no output; older withdrawal still changes the authority.
			case "withdraw":
				err = db.Transaction(func(tx *gorm.DB) error {
					if err := graph.Lock(tx, 1); err != nil {
						return err
					}
					return graph.WithdrawTx(tx, []string{one.ID})
				})
			case "delete_entry":
				err = svc.repo.DeleteEntry(ctx, 1, one.ID)
			case "delete_relation":
				err = entities.DeleteRelation(ctx, graphRows(t, db)[0].ID)
			case "merge_entity":
				err = entities.MergeEntities(ctx, "d", "a")
			case "merge_type":
				err = entities.MergeRelationTypes(ctx, "entity-library", "合作", "投资")
			case "fallback":
				err = svc.repo.DeleteEntry(ctx, 1, two.ID)
			}
			require.ErrorContains(t, err, "ikb12 pending unavailable")
			require.Equal(t, before, graphSnapshot(t, db))
		})
	}
}
func TestIKB12ContentTypeCorrectionPreservesGraphAndRollsBackOnAuditFailure(t *testing.T) {
	t.Parallel()
	db, ctx, _, svc := graphFixture(t)
	entry := graphEntry(t, db, "one", "", time.Now())
	graphPublish(t, db, ctx, svc, entry, graphInput("a", "合作", "b"))
	rows, pending := graphRows(t, db), graphPending(t, db)
	// Classification is a label; changing it neither withdraws nor re-extracts relations.
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_ikb12_enqueue() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'unexpected enqueue'; END $$; CREATE TRIGGER reject_ikb12_enqueue BEFORE INSERT ON task_pending_ops FOR EACH ROW EXECUTE FUNCTION reject_ikb12_enqueue()`).Error)
	updated, err := svc.CorrectContentType(ctx, entry.ID, "off")
	require.NoError(t, err)
	require.Equal(t, "off", updated.ContentType)
	require.True(t, updated.ContentTypeCorrected)
	require.Equal(t, types.ParseStatusCompleted, updated.ParseStatus)
	require.Equal(t, rows, graphRows(t, db))
	require.Equal(t, pending, graphPending(t, db))
	before := graphSnapshot(t, db)
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_ikb12_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'ikb12 log unavailable'; END $$; CREATE TRIGGER reject_ikb12_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_ikb12_log()`).Error)
	_, err = svc.CorrectContentType(ctx, entry.ID, "on")
	require.ErrorContains(t, err, "ikb12 log unavailable")
	require.Equal(t, before, graphSnapshot(t, db))
}

func TestIKB12LogFailureRollsBackAlreadyWrittenSyncRecords(t *testing.T) {
	t.Parallel()
	db, ctx, entities, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"))
	before := graphSnapshot(t, db)
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_ikb12_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'ikb12 log unavailable'; END $$; CREATE TRIGGER reject_ikb12_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_ikb12_log()`).Error)
	require.ErrorContains(t, entities.DeleteRelation(ctx, graphRows(t, db)[0].ID), "ikb12 log unavailable")
	require.Equal(t, before, graphSnapshot(t, db))
	require.ErrorContains(t, entities.MergeRelationTypes(ctx, "entity-library", "合作", "投资"), "ikb12 log unavailable")
	require.Equal(t, before, graphSnapshot(t, db))
}
func TestIKB12ReplacementKeepsIdentityAndRemovesObsoleteEvidence(t *testing.T) {
	t.Parallel()
	db, ctx, _, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	two := graphEntry(t, db, "two", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"), graphInput("a", "投资", "d"))
	graphPublish(t, db, ctx, svc, two, graphInput("a", "合作", "b"))
	var first types.Relation
	require.NoError(t, db.Where("relation_type='合作'").First(&first).Error)
	input := graphInput("a", "合作", "b")
	input.Evidence[0].Locations = []types.SourceLocation{{Start: 4, End: 8}}
	before := len(graphPending(t, db))
	graphPublish(t, db, ctx, svc, one, input)
	rows := graphRows(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, first.ID, rows[0].ID)
	require.Greater(t, rows[0].Version, first.Version)
	require.Greater(t, len(graphPending(t, db)), before)
	for _, source := range graphSources(t, db, first.ID) {
		if source.EntryID == one.ID {
			require.Len(t, source.Evidence, 1)
			require.Equal(t, 4, source.Evidence[0].Locations[0].Start)
		}
	}
}
func TestIKB12CatalogUpdateRetainsReferencedTypesAndAliases(t *testing.T) {
	t.Parallel()
	db, ctx, entities, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"))
	require.NoError(t, entities.MergeRelationTypes(ctx, "entity-library", "合作", "投资"))
	repo := repository.NewLibraryRepository(db)
	library, err := repo.GetLibraryByID(ctx, "entity-library")
	require.NoError(t, err)
	library.RelationTypes = []types.LibraryRelationType{{Name: "合作", Enabled: true}, {Name: "投资", Enabled: true}}
	require.NoError(t, repo.UpdateLibrary(ctx, library))
	var old types.LibraryRelationType
	require.NoError(t, db.Where("library_id=? AND name=?", library.ID, "合作").First(&old).Error)
	require.False(t, old.Enabled)
	library.RelationTypes = []types.LibraryRelationType{}
	require.NoError(t, repo.UpdateLibrary(ctx, library))
	require.Len(t, graphRows(t, db), 1)
	var merges []types.RelationTypeMerge
	require.NoError(t, db.Find(&merges).Error)
	require.Len(t, merges, 1)
}

func TestIKB12DeletionFallbackRespectsSuppressionAndBodyHash(t *testing.T) {
	t.Parallel()
	db, ctx, entities, svc := graphFixture(t)
	now := time.Now().Add(-time.Hour)
	old := graphEntry(t, db, "old", "source", now)
	graphPublish(t, db, ctx, svc, old, graphInput("a", "合作", "b"), graphInput("a", "持有", "b"))
	for _, row := range graphRows(t, db) {
		if row.RelationType == "合作" {
			require.NoError(t, entities.DeleteRelation(ctx, row.ID))
		}
	}
	latest := graphEntry(t, db, "latest", "source", now.Add(time.Minute))
	graphPublish(t, db, ctx, svc, latest, graphInput("d", "投资", "b"))
	require.NoError(t, svc.repo.DeleteEntry(ctx, 1, latest.ID))
	require.Len(t, graphRows(t, db), 1)
	require.Equal(t, "持有", graphRows(t, db)[0].RelationType)
	// A stored output for another body is never restored.
	require.NoError(t, db.Model(&types.Entry{}).Where("id=?", old.ID).Update("vault_body", "changed stored body").Error)
	latest = graphEntry(t, db, "latest2", "source", now.Add(2*time.Minute))
	graphPublish(t, db, ctx, svc, latest, graphInput("d", "投资", "b"))
	require.NoError(t, svc.repo.HardDeleteEntry(ctx, 1, latest.ID))
	require.Empty(t, graphRows(t, db))
}
func TestIKB12RepositoryDeletionCannotWithdrawAnotherSpace(t *testing.T) {
	t.Parallel()
	db, ctx, _, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"))
	before := graphSnapshot(t, db)
	require.NoError(t, svc.repo.DeleteEntry(ctx, 2, one.ID))
	require.Equal(t, before, graphSnapshot(t, db))
	require.NoError(t, svc.repo.HardDeleteEntry(ctx, 2, one.ID))
	require.Equal(t, before, graphSnapshot(t, db))
}

func TestIKB12PublicationRejectsStaleSavedBody(t *testing.T) {
	t.Parallel()
	db, ctx, _, svc := graphFixture(t)
	one := graphEntry(t, db, "one", "", time.Now())
	graphPublish(t, db, ctx, svc, one, graphInput("a", "合作", "b"))
	before := graphRows(t, db)
	pending := graphPending(t, db)
	require.NoError(t, db.Model(&types.Entry{}).Where("id=?", one.ID).Update("vault_body", "changed body").Error)
	require.ErrorContains(t, svc.publishEntryRelations(ctx, one.ID, "run-one"), "processing body changed")
	require.Equal(t, before, graphRows(t, db))
	require.Equal(t, pending, graphPending(t, db))
}
