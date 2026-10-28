package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type intakeDeleteRepo struct {
	interfaces.EntryRepository
	entry   *types.Entry
	deleted bool
}

func TestDeleteArticleRemovesEntityAssociationsAndRetainsEntity(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Delete fixture"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "library", SpaceID: 1, Name: "Delete fixture", Template: types.TemplateOwn, Directory: "delete-fixture"}).Error)
	article := &types.Entry{ID: "article", SpaceID: 1, LibraryID: "library", ParseStatus: types.ParseStatusCompleted}
	entity := &types.Entity{ID: "entity", SpaceID: 1, LibraryID: article.LibraryID, CanonicalName: "Acme", NormalizedName: "acme", EntityType: "company"}
	require.NoError(t, db.Create(article).Error)
	require.NoError(t, db.Create(entity).Error)
	require.NoError(t, db.Create(&types.EntryEntity{SpaceID: 1, EntryID: article.ID, EntityID: entity.ID}).Error)
	svc := &entryService{
		db: db, repo: repository.NewEntryRepository(db), libraryService: intakeDeleteLibrary{},
		fileSvc: &intakeFileService{}, chunkService: intakeDeleteChunks{repo: intakeDeleteChunkRepo{}},
		spaceRepo: intakeSpaceRepo{},
	}
	ctx := context.WithValue(context.Background(), types.SpaceInfoContextKey, &types.Space{ID: 1})
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, uint64(1))
	require.NoError(t, svc.DeleteEntryList(ctx, []string{article.ID}))
	var count int64
	require.NoError(t, db.Model(&types.EntryEntity{}).Where("entry_id = ?", article.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.First(&types.Entity{}, "id = ?", entity.ID).Error)
	require.Error(t, db.First(&types.Entry{}, "id = ?", article.ID).Error)
}

func (r *intakeDeleteRepo) GetEntryBatch(context.Context, uint64, []string) ([]*types.Entry, error) {
	return []*types.Entry{r.entry}, nil
}
func (r *intakeDeleteRepo) UpdateEntry(context.Context, *types.Entry) error { return nil }
func (r *intakeDeleteRepo) DeleteEntryList(context.Context, uint64, []string) error {
	r.deleted = true
	return nil
}

func (intakeDeleteChunkRepo) ListImageInfoByEntryIDs(context.Context, uint64, []string) ([]interfaces.ChunkImageInfo, error) {
	return nil, nil
}

type intakeDeleteChunks struct {
	interfaces.ChunkService
	repo interfaces.ChunkRepository
}

func (s intakeDeleteChunks) GetRepository() interfaces.ChunkRepository       { return s.repo }
func (intakeDeleteChunks) DeleteByEntryList(context.Context, []string) error { return nil }

type intakeDeleteLibrary struct {
	interfaces.LibraryService
	creator string
}

func (s intakeDeleteLibrary) GetLibraryByID(context.Context, string) (*types.Library, error) {
	return &types.Library{ID: "library", CreatorID: s.creator}, nil
}

func TestDeleteEntryListIntakeAndResources(t *testing.T) {
	makeEntry := func() *types.Entry {
		k := &types.Entry{ID: "k", SpaceID: 1, LibraryID: "library", FilePath: "upload", ParseStatus: types.ParseStatusRejected}
		require.NoError(t, k.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "rejected", OriginalPath: "original", Images: []string{"image"}}))
		return k
	}
	for _, tt := range []struct {
		name, user string
		wantErr    bool
	}{{"personal user cleans", "creator", false}} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &intakeDeleteRepo{entry: makeEntry()}
			files := &intakeFileService{saved: map[string][]byte{"upload": {}, "original": {}, "image": {}}}
			svc := &entryService{repo: repo, libraryService: intakeDeleteLibrary{creator: "creator"}, fileSvc: files, chunkService: intakeDeleteChunks{repo: intakeDeleteChunkRepo{}}, spaceRepo: intakeSpaceRepo{}}
			ctx := context.WithValue(context.Background(), types.SpaceInfoContextKey, &types.Space{ID: 1})
			ctx = context.WithValue(ctx, types.SpaceIDContextKey, uint64(1))
			ctx = context.WithValue(ctx, types.UserIDContextKey, tt.user)
			err := svc.DeleteEntryList(ctx, []string{"k"})
			if tt.wantErr {
				require.Error(t, err)
				require.False(t, repo.deleted)
				require.Empty(t, files.deleted)
				return
			}
			require.NoError(t, err)
			require.True(t, repo.deleted)
			require.ElementsMatch(t, []string{"upload", "original", "image"}, files.deleted)
		})
	}
}

type intakeDeleteChunkRepo struct{ interfaces.ChunkRepository }
