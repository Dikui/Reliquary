package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type clearSessionRepo struct{ interfaces.SessionRepository }

func (clearSessionRepo) Get(context.Context, uint64, string, string) (*types.Session, error) {
	return &types.Session{ID: "session", TenantID: 1}, nil
}

type clearMessageRepo struct {
	interfaces.MessageRepository
	knowledgeIDs []string
	lookupErr    error
	deleted      bool
}

func (r *clearMessageRepo) GetKnowledgeIDsBySessionID(context.Context, string) ([]string, error) {
	return r.knowledgeIDs, r.lookupErr
}

func (r *clearMessageRepo) DeleteMessagesBySessionID(context.Context, string) error {
	r.deleted = true
	r.knowledgeIDs = nil
	return nil
}

type clearKnowledgeService struct {
	interfaces.KnowledgeService
	deleted chan []string
}

func (s clearKnowledgeService) DeleteKnowledgeList(_ context.Context, ids []string) error {
	s.deleted <- ids
	return nil
}

func TestClearSessionMessagesCapturesKnowledgeIDsBeforeDeletion(t *testing.T) {
	for _, lookupErr := range []error{nil, errors.New("knowledge lookup failed")} {
		name := "cleanup captured knowledge"
		if lookupErr != nil {
			name = "lookup failure preserves messages"
		}
		t.Run(name, func(t *testing.T) {
			repo := &clearMessageRepo{knowledgeIDs: []string{"knowledge"}, lookupErr: lookupErr}
			knowledge := clearKnowledgeService{deleted: make(chan []string, 1)}
			svc := &messageService{messageRepo: repo, sessionRepo: clearSessionRepo{}, knowService: knowledge}
			ctx := context.WithValue(t.Context(), types.TenantIDContextKey, uint64(1))
			err := svc.ClearSessionMessages(ctx, "session")
			if lookupErr != nil {
				require.ErrorIs(t, err, lookupErr)
				require.False(t, repo.deleted)
				return
			}
			require.NoError(t, err)
			require.True(t, repo.deleted)
			select {
			case ids := <-knowledge.deleted:
				require.Equal(t, []string{"knowledge"}, ids)
			case <-time.After(time.Second):
				t.Fatal("captured knowledge IDs were not cleaned up")
			}
		})
	}
}
