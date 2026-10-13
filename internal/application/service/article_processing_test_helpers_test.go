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

func validProcessingFixture(contentType string) string {
	result := map[string]interface{}{"content_type": contentType, "summary": map[string]interface{}{"overview": "Acme reports an original product announcement and its deployment capabilities.", "points": []string{"The product has been announced.", "Deployment capabilities are described.", "Original measurements are supplied."}, "key_terms": []string{"Acme", "widget"}}, "entities": []interface{}{}, "topics": []string{}}
	raw, _ := json.Marshal(result)
	return string(raw)
}
