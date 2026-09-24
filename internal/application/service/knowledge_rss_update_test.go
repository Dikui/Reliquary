package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type rssCandidateSequenceRepo struct {
	interfaces.KnowledgeRepository
	reads int
}

func (r *rssCandidateSequenceRepo) GetKnowledgeByID(context.Context, uint64, string) (*types.Knowledge, error) {
	r.reads++
	if r.reads == 1 {
		return &types.Knowledge{ParseStatus: types.ParseStatusFinalizing, EnableStatus: "disabled"}, nil
	}
	return &types.Knowledge{ParseStatus: types.ParseStatusCompleted, EnableStatus: "disabled",
		Description: "completed summary", Metadata: types.JSON(`{"rss_update_candidate":"true"}`)}, nil
}

func TestWaitRSSUpdateCandidateUsesFinalizedRow(t *testing.T) {
	repo := &rssCandidateSequenceRepo{}
	svc := &knowledgeService{repo: repo}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	candidate, err := svc.waitRSSUpdateCandidate(ctx, 1, "candidate")
	require.NoError(t, err)
	require.Equal(t, 2, repo.reads)
	require.Equal(t, "completed summary", candidate.Description)
	require.Equal(t, types.JSON(`{"rss_update_candidate":"true"}`), candidate.Metadata)
}
