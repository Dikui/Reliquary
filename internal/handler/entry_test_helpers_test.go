package handler

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type stubArticleKBService struct {
	interfaces.KnowledgeBaseService
}

func (s *stubArticleKBService) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if id != "kb-1" {
		return nil, errors.New("knowledge base not found")
	}
	return &types.KnowledgeBase{ID: "kb-1", SpaceID: 1, Type: types.KnowledgeBaseTypeDocument}, nil
}

func (s *stubArticleKBService) GetKnowledgeBase(ctx context.Context) (*types.KnowledgeBase, error) {
	return s.GetKnowledgeBaseByID(ctx, "kb-1")
}
