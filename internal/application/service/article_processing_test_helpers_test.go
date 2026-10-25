package service

import (
	"context"
	"encoding/json"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type summaryStateModelService struct {
	interfaces.ModelService
	model    chat.Chat
	calls    int
	err      error
	metadata *types.Model
}

func (s *summaryStateModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	s.calls++
	return s.model, s.err
}

func (s *summaryStateModelService) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	if s.metadata != nil {
		return s.metadata, s.err
	}
	return &types.Model{ID: id, Type: types.ModelTypeGeneration}, s.err
}

func validProcessingFixture() string {
	result := map[string]interface{}{
		"title": "Acme announces a widget",
		"summary": map[string]interface{}{
			"overview": "Acme reports an original product announcement and its deployment capabilities.",
		},
		"domain_ids": []string{},
		"entity_ids": []string{},
	}
	raw, _ := json.Marshal(result)
	return string(raw)
}
