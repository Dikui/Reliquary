package service

import (
	"reliquary/internal/modelcalls"
	"reliquary/internal/models/provider"
	"reliquary/internal/types"
)

func (s *modelService) callAccounting(m *types.Model) *types.ModelCallAccounting {
	if s.db == nil {
		return nil
	}
	snapshot := *m
	switch m.Source {
	case types.ModelSourceChatGPT:
		snapshot.Parameters.Provider = "chatgpt"
	case types.ModelSourceLocal:
		snapshot.Parameters.Provider = "ollama"
		if m.Type == types.ModelTypeRerank {
			snapshot.Parameters.Provider = string(provider.Resolve(provider.ProviderName(m.Parameters.Provider), m.Parameters.BaseURL))
		}
	default:
		snapshot.Parameters.Provider = string(provider.Resolve(provider.ProviderName(m.Parameters.Provider), m.Parameters.BaseURL))
	}
	return &types.ModelCallAccounting{Model: &snapshot, Write: modelcalls.NewStore(s.db).Enqueue}
}

// ModelCallAccounting also supplies the ledger for temporary connection probes.
func (s *modelService) ModelCallAccounting(m *types.Model) *types.ModelCallAccounting {
	return s.callAccounting(m)
}
