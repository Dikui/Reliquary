package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type intakeDeleteRepo struct {
	interfaces.KnowledgeRepository
	knowledge *types.Knowledge
	deleted   bool
}

func (r *intakeDeleteRepo) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{r.knowledge}, nil
}
func (r *intakeDeleteRepo) UpdateKnowledge(context.Context, *types.Knowledge) error { return nil }
func (r *intakeDeleteRepo) DeleteKnowledgeList(context.Context, uint64, []string) error {
	r.deleted = true
	return nil
}
func (r *intakeDeleteRepo) DeleteKnowledgeTagRelations(context.Context, string) error { return nil }

type intakeDeleteChunkRepo struct{ interfaces.ChunkRepository }

func (intakeDeleteChunkRepo) ListImageInfoByKnowledgeIDs(context.Context, uint64, []string) ([]interfaces.ChunkImageInfo, error) {
	return nil, nil
}

type intakeDeleteChunks struct {
	interfaces.ChunkService
	repo interfaces.ChunkRepository
}

func (s intakeDeleteChunks) GetRepository() interfaces.ChunkRepository           { return s.repo }
func (intakeDeleteChunks) DeleteByKnowledgeList(context.Context, []string) error { return nil }

type intakeDeleteKB struct {
	interfaces.KnowledgeBaseService
	creator string
}

func (s intakeDeleteKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb", CreatorID: s.creator}, nil
}

func TestDeleteKnowledgeListIntakeOwnershipAndResources(t *testing.T) {
	makeKnowledge := func() *types.Knowledge {
		k := &types.Knowledge{ID: "k", TenantID: 1, KnowledgeBaseID: "kb", FilePath: "upload", ParseStatus: types.ParseStatusRejected}
		require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "rejected", OriginalPath: "original", Images: []string{"image"}}))
		return k
	}
	for _, tt := range []struct {
		name, user string
		wantErr    bool
	}{{"creator cleans", "creator", false}, {"non creator rejected", "other", true}} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &intakeDeleteRepo{knowledge: makeKnowledge()}
			files := &intakeFileService{saved: map[string][]byte{"upload": {}, "original": {}, "image": {}}}
			svc := &knowledgeService{repo: repo, kbService: intakeDeleteKB{creator: "creator"}, fileSvc: files, chunkService: intakeDeleteChunks{repo: intakeDeleteChunkRepo{}}, tenantRepo: intakeTenantRepo{}}
			ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, &types.Tenant{ID: 1})
			ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))
			ctx = context.WithValue(ctx, types.UserIDContextKey, tt.user)
			err := svc.DeleteKnowledgeList(ctx, []string{"k"})
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
