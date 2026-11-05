package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

// Medium regression, MEL-07: a stale extraction plan must not claim an alias
// concurrently with another entity after its owner was merged across types.
func TestEntityConcurrentAliasOwnershipAfterCrossTypeMerge(t *testing.T) {
	testEntityConcurrentAliasOwnership(t, true)
}

// Medium regression, MEL-07: two documents adding the same alias to distinct
// entities of one extracted type retain the first committed owner.
func TestEntityConcurrentAliasOwnershipWithinExtractedType(t *testing.T) {
	testEntityConcurrentAliasOwnership(t, false)
}

func testEntityConcurrentAliasOwnership(t *testing.T, mergeDuringJudgment bool) {
	t.Helper()
	db, ctx, svc := entityFixture(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	keep := entityRecord("z-keep", "Lumen Company", "company")
	sourceType := "company"
	if mergeDuringJudgment {
		sourceType = "organization"
	}
	source := entityRecord("source", "Lumen Research", sourceType)
	other := entityRecord("a-other", "Other Company", "company")
	require.NoError(t, db.Create(&[]types.Entity{keep, source, other}).Error)
	first := entityArticle(t, db, "first-extraction")
	second := entityArticle(t, db, "second-extraction")
	probe := entityArticle(t, db, "alias-resolution")
	judging := make(chan struct{})
	resume := make(chan struct{})
	// Replaces the external identity model and pauses the first document after
	// candidate recall so a manual merge can change its persisted identity type.
	svc.models = entityTestModels{model: &entityCountingChat{reply: func(context.Context, []chat.Message) string {
		close(judging)
		select {
		case <-resume:
		case <-ctx.Done():
		}
		return `{"decision":"match","entity_id":"source","confidence":1}`
	}}}
	results := make(chan error, 2)
	go func() {
		_, err := svc.ResolveEntities(ctx, first.ID, []types.EntityCandidate{{Name: "Lumen Research Institute", EntityType: sourceType, Aliases: []string{"Shared signal"}}})
		results <- err
	}()
	select {
	case <-judging:
	case <-ctx.Done():
		t.Fatal("first extraction did not reach identity judgment")
	}
	wantOwner := source.ID
	if mergeDuringJudgment {
		require.NoError(t, svc.MergeEntities(ctx, keep.ID, source.ID))
		wantOwner = keep.ID
	}

	// Hold the first alias INSERT after its conflict check. The second writer
	// must either wait for ownership or observe the committed alias afterward.
	gate := db.WithContext(ctx).Begin()
	require.NoError(t, gate.Error)
	defer gate.Rollback()
	require.NoError(t, gate.Exec("SELECT pg_advisory_xact_lock(hashtextextended('mel07-alias-gate', 0))").Error)
	require.NoError(t, db.Exec(`CREATE FUNCTION gate_shared_alias() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
  IF NEW.normalized_alias='shared signal' THEN
   PERFORM pg_advisory_xact_lock(hashtextextended('mel07-alias-gate', 0));
  END IF;
  RETURN NEW;
 END $$;
 CREATE TRIGGER gate_shared_alias BEFORE INSERT ON entity_aliases FOR EACH ROW EXECUTE FUNCTION gate_shared_alias()`).Error)
	close(resume)
	waitForWriters := func(count int) {
		t.Helper()
		require.Eventually(t, func() bool {
			var waiting int
			err := db.WithContext(ctx).Raw("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'").Scan(&waiting).Error
			return err == nil && waiting == count
		}, 5*time.Second, 10*time.Millisecond, "extraction transactions must overlap")
	}
	waitForWriters(1)
	go func() {
		_, err := svc.ResolveEntities(ctx, second.ID, []types.EntityCandidate{{Name: other.CanonicalName, EntityType: "company", Aliases: []string{"Shared signal"}}})
		results <- err
	}()
	waitForWriters(2)
	require.NoError(t, gate.Commit().Error)
	for range 2 {
		require.NoError(t, <-results)
	}
	var aliases []types.EntityAlias
	require.NoError(t, db.Where("entity_type=? AND normalized_alias=?", "company", "shared signal").Find(&aliases).Error)
	assert.Len(t, aliases, 1, "one extracted-library alias must have one owner per type")
	resolved, err := svc.ResolveEntities(ctx, probe.ID, []types.EntityCandidate{{Name: "Shared signal", EntityType: "company"}})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, wantOwner, resolved[0].ID, "alias lookup must retain the first owner's identity instead of selecting by ID")
}

// Medium regression, MEL-07: extracted canonical names remain reserved after
// cross-type merges; reusing one would redirect future mentions to a new owner.
func TestEntityResolutionMergedCanonicalNameRemainsReserved(t *testing.T) {
	db, ctx, svc := entityFixture(t)
	keep := entityRecord("keep", "Lumen Organization", "organization")
	source := entityRecord("source", "Legacy Company", "company")
	other := entityRecord("other", "Replacement Company", "company")
	require.NoError(t, db.Create(&[]types.Entity{keep, source, other}).Error)
	require.NoError(t, svc.MergeEntities(ctx, keep.ID, source.ID))
	_, err := svc.RenameEntity(ctx, other.ID, " LEGACY COMPANY ")
	assert.ErrorContains(t, err, "conflicts", "a merged extracted identity must keep its original name reserved")
	// Replaces the external identity model. A missed old name can produce a
	// creation plan, but persistence must still follow its merged identity.
	svc.models = entityTestModels{model: &entityCountingChat{reply: func(context.Context, []chat.Message) string {
		return `{"decision":"new","confidence":1}`
	}}}
	article := entityArticle(t, db, "merged-name-resolution")
	resolved, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: "Legacy Company", EntityType: "company"}})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, keep.ID, resolved[0].ID, "old canonical mentions must continue following the merge")
	detail, err := svc.EntityDetail(ctx, other.ID)
	require.NoError(t, err)
	assert.Equal(t, "Replacement Company", detail.Entity.CanonicalName, "the failed rename must leave the other identity unchanged")
}

// Medium spec, MEL-07 / ADR 0022: managed archival releases active spellings
// while each historical owner retains its aliases.
func TestManagedEntityArchivedNamesAndAliasesKeepSeparateOwners(t *testing.T) {
	db, ctx, svc := managedHistoryFixture(t)
	original, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Reusable product", Kind: "product", Aliases: []string{"Reusable signal"}})
	require.NoError(t, err)
	archived := true
	original, err = svc.UpdateManagedEntity(ctx, original.ID, types.ManagedEntityPatch{Revision: original.Revision, Archived: &archived})
	require.NoError(t, err)
	replacement, err := svc.CreateManagedEntity(ctx, "entity-library", types.ManagedEntityInput{Name: "Reusable product", Kind: "product", Aliases: []string{"Reusable signal"}})
	require.NoError(t, err)
	for _, id := range []string{original.ID, replacement.ID} {
		detail, err := svc.EntityDetail(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "Reusable product", detail.Entity.CanonicalName)
		require.ElementsMatch(t, []string{"Reusable product", "Reusable signal"}, detail.Entity.Aliases)
	}
	article := entityArticle(t, db, "managed-alias-resolution")
	require.NoError(t, db.Model(article).Update("vault_body", "Reusable product uses Reusable signal.").Error)
	for _, name := range []string{"Reusable product", "Reusable signal"} {
		resolved, err := svc.ResolveEntities(ctx, article.ID, []types.EntityCandidate{{Name: name, EntityType: "product"}})
		require.NoError(t, err)
		require.Len(t, resolved, 1)
		require.Equal(t, replacement.ID, resolved[0].ID, "only the active managed owner may match a reused spelling")
	}
}
