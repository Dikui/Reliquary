package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

// Regression (medium): MEL-05. Retiring a type must preserve the validity and
// structural meaning of historical relationships, while active ones block it.
func TestManagedCatalogRetiresOnlyEndedRelationTypes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		ended        bool
		enabled      bool
		hierarchical bool
		fieldType    string
		wantError    string
	}{
		{name: "ended relationships allow retirement", ended: true, fieldType: "text"},
		{name: "active relationships block retirement", fieldType: "text", wantError: "still in use"},
		{name: "ended attributes constrain retired types", ended: true, fieldType: "number", wantError: "relationship attributes"},
		{name: "ended attributes constrain enabled types", ended: true, enabled: true, fieldType: "number", wantError: "relationship attributes"},
		{name: "ended structure constrains retired types", ended: true, hierarchical: true, fieldType: "text", wantError: "historical structure"},
		{name: "ended structure constrains enabled types", ended: true, enabled: true, hierarchical: true, fieldType: "text", wantError: "historical structure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, ctx, svc := managedHistoryFixture(t)
			libraries := NewLibraryService(db, repository.NewLibraryRepository(db), nil, nil, nil, nil, nil)
			library, err := libraries.CreateLibrary(ctx, types.CreateLibraryRequest{
				Name: "Products", Directory: "products", EntityMode: "managed",
				EntitySchema: types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product", ParentKinds: []string{"product"}}}},
			})
			require.NoError(t, err)
			rules := []types.LibraryRelationType{{Name: "compatible", Enabled: true, Fields: []types.EntityFieldDefinition{{Key: "note", Name: "Note", Type: "text"}}}}
			_, err = libraries.ConfigureLibrary(ctx, library.ID, types.LibraryPatch{RelationTypes: &rules})
			require.NoError(t, err)
			subject, err := svc.CreateManagedEntity(ctx, library.ID, types.ManagedEntityInput{Name: "Product A", Kind: "product"})
			require.NoError(t, err)
			object, err := svc.CreateManagedEntity(ctx, library.ID, types.ManagedEntityInput{Name: "Product B", Kind: "product"})
			require.NoError(t, err)
			relation, err := svc.SaveManagedRelation(ctx, library.ID, "", types.ManagedRelationInput{
				SubjectID: subject.ID, ObjectID: object.ID, RelationType: "compatible", Attributes: map[string]any{"note": "tested"},
			})
			require.NoError(t, err)
			asOf := time.Now()
			if tc.ended {
				require.NoError(t, svc.DeleteManagedRelation(ctx, relation.ID, relation.Version))
			}
			rules[0].Enabled, rules[0].Hierarchical, rules[0].Fields[0].Type = tc.enabled, tc.hierarchical, tc.fieldType
			_, err = libraries.ConfigureLibrary(ctx, library.ID, types.LibraryPatch{RelationTypes: &rules})
			if tc.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantError)
			}
			stored, err := libraries.GetLibraryByID(ctx, library.ID)
			require.NoError(t, err)
			require.Len(t, stored.RelationTypes, 1)
			require.Equal(t, tc.wantError != "", stored.RelationTypes[0].Enabled)
			require.False(t, stored.RelationTypes[0].Hierarchical)
			require.Equal(t, "text", stored.RelationTypes[0].Fields[0].Type)
			history, err := svc.ListRelations(ctx, subject.ID, library.ID, asOf)
			require.NoError(t, err)
			require.Len(t, history, 1)
			require.Len(t, history[0].Relations, 1)
			require.Equal(t, relation.ID, history[0].Relations[0].ID)
			require.Equal(t, "tested", history[0].Relations[0].Attributes["note"])
		})
	}
}

// Regression (medium): MEL-05. Only excluded document links may be removed
// with a product; the deletion log retains those decisions in the same transaction.
func TestDeleteManagedEntityWithOnlyExcludedLinks(t *testing.T) {
	for _, origin := range []string{"manual", "extracted"} {
		t.Run(origin, func(t *testing.T) {
			db, ctx, svc := managedHistoryFixture(t)
			entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Mistaken product", Kind: "product"})
			require.NoError(t, err)
			other, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Retained product", Kind: "product"})
			require.NoError(t, err)
			first, second := entityArticle(t, db, "first-paper"), entityArticle(t, db, "second-paper")
			for _, entry := range []*types.Entry{first, second} {
				// Seed an existing manual or automatically extracted document link.
				require.NoError(t, db.Create(&types.EntryEntity{SpaceID: 1, EntryID: entry.ID, EntityID: entity.ID, Origin: origin, RelationType: "specification"}).Error)
			}
			require.NoError(t, svc.SetEntryEntityLink(ctx, first.ID, other.ID, false))
			blockedErr := svc.DeleteManagedEntity(ctx, entity.ID, entity.Revision)
			require.Error(t, blockedErr)
			require.NoError(t, svc.SetEntryEntityLink(ctx, first.ID, entity.ID, true))
			require.Error(t, svc.DeleteManagedEntity(ctx, entity.ID, entity.Revision), "the second document still references the product")
			logs, err := NewOperationLogService(db).List(ctx, "delete_entity", 1, 20)
			require.NoError(t, err)
			require.Zero(t, logs.Total)
			require.NoError(t, svc.SetEntryEntityLink(ctx, second.ID, entity.ID, true))
			excluded := make(map[string]ManagedEntityLink)
			for _, entry := range []*types.Entry{first, second} {
				links, err := svc.EntryEntityLinks(ctx, entry.ID)
				require.NoError(t, err)
				for _, link := range links {
					if link.Entity.ID == entity.ID {
						excluded[entry.ID] = link
					}
				}
			}
			require.Len(t, excluded, 2)
			require.NoError(t, svc.DeleteManagedEntity(ctx, entity.ID, entity.Revision))
			require.ErrorContains(t, blockedErr, "exclude")
			require.ErrorContains(t, blockedErr, "archive")
			_, err = svc.EntityDetail(ctx, entity.ID)
			require.Error(t, err)
			links, err := svc.EntryEntityLinks(ctx, first.ID)
			require.NoError(t, err)
			require.Len(t, links, 1)
			require.Equal(t, other.ID, links[0].Entity.ID)
			require.False(t, links[0].Excluded)
			links, err = svc.EntryEntityLinks(ctx, second.ID)
			require.NoError(t, err)
			require.Empty(t, links)
			logs, err = NewOperationLogService(db).List(ctx, "delete_entity", 1, 20)
			require.NoError(t, err)
			require.EqualValues(t, 1, logs.Total)
			require.Equal(t, entity.ID, logs.Items[0].ObjectID)
			var before struct {
				types.Entity
				ExcludedLinks []types.EntryEntity `json:"excluded_links"`
			}
			require.NoError(t, json.Unmarshal(logs.Items[0].Before, &before))
			require.Equal(t, entity.ID, before.ID)
			require.Equal(t, entity.CanonicalName, before.CanonicalName)
			require.Len(t, before.ExcludedLinks, 2)
			for _, link := range before.ExcludedLinks {
				previous, ok := excluded[link.EntryID]
				require.True(t, ok)
				require.Equal(t, entity.ID, link.EntityID)
				require.EqualValues(t, 1, link.SpaceID)
				require.True(t, link.Excluded)
				require.Equal(t, previous.Origin, link.Origin)
				require.Equal(t, previous.RelationType, link.RelationType)
				require.Equal(t, previous.BodyHash, link.BodyHash)
			}
		})
	}

	t.Run("audit failure rolls back excluded links", func(t *testing.T) {
		db, ctx, svc := managedHistoryFixture(t)
		entity, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Retained on failure", Kind: "product"})
		require.NoError(t, err)
		entry := entityArticle(t, db, "excluded-paper")
		require.NoError(t, svc.SetEntryEntityLink(ctx, entry.ID, entity.ID, true))
		// Simulate storage rejecting the audit write after the entity and links are deleted.
		require.NoError(t, db.Exec(`CREATE FUNCTION reject_entity_delete_log() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.operation_type = 'delete_entity' THEN RAISE EXCEPTION 'audit write unavailable'; END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER reject_entity_delete_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_entity_delete_log()`).Error)
		require.ErrorContains(t, svc.DeleteManagedEntity(ctx, entity.ID, entity.Revision), "audit write unavailable")
		detail, err := svc.EntityDetail(ctx, entity.ID)
		require.NoError(t, err)
		require.Equal(t, entity.Revision, detail.Entity.Revision)
		history, err := svc.EntityAttributeHistory(ctx, entity.ID, 1, 20)
		require.NoError(t, err)
		require.EqualValues(t, 1, history.Total)
		links, err := svc.EntryEntityLinks(ctx, entry.ID)
		require.NoError(t, err)
		require.Len(t, links, 1)
		require.Equal(t, entity.ID, links[0].Entity.ID)
		require.True(t, links[0].Excluded)
		logs, err := NewOperationLogService(db).List(ctx, "delete_entity", 1, 20)
		require.NoError(t, err)
		require.Zero(t, logs.Total)
	})
}
