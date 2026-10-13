package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/chatgpt"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils/ollama"
)

// NewModelServiceWithChatGPT shares the authorization manager with settings.
func NewModelServiceWithChatGPT(repo interfaces.ModelRepository, libraryRepo interfaces.LibraryRepository, ollamaService *ollama.OllamaService, pooler embedding.EmbedderPooler, spaceService interfaces.SpaceService, db *gorm.DB, manager *chatgpt.Manager, policy *ChatGPTPolicyService) interfaces.ModelService {
	s := NewModelServiceWithRoutingDB(repo, libraryRepo, ollamaService, pooler, spaceService, db).(*modelService)
	s.chatGPT, s.chatGPTPolicy = manager, policy
	return s
}

// SetChatGPTTransport supplies a test transport while keeping official URLs.
// Production assembly leaves this unset and uses the existing SSRF-safe client.
func (s *modelService) SetChatGPTTransport(client *http.Client) { s.chatGPTHTTPClient = client }

func (s *modelService) validateChatGPTModel(ctx context.Context, m *types.Model) error {
	if m.Source != types.ModelSourceChatGPT {
		return nil
	}
	if m.Type != types.ModelTypeGeneration {
		return errors.New("ChatGPT plan supports generation models only; configure a separate embedding model")
	}
	if s.db == nil {
		return errors.New("ChatGPT authorization database is unavailable")
	}
	manager := s.chatGPT
	if manager == nil {
		manager = chatgpt.NewManager(s.db)
	}
	accountID := m.Parameters.ExtraConfig["chatgpt_account_id"]
	if accountID == "" {
		return errors.New("select a ChatGPT account")
	}
	if _, err := manager.Status(ctx, accountID); err != nil {
		return err
	}
	// OAuth credentials belong to the account registration, never the model.
	m.Parameters.APIKey, m.Parameters.AppSecret, m.Parameters.BaseURL = "", "", ""
	m.Parameters.Provider, m.Parameters.InterfaceType = "chatgpt", "responses"
	m.Parameters.CustomHeaders = nil
	return nil
}

func (s *modelService) chatGPTModel(ctx context.Context, m *types.Model) (chat.Chat, error) {
	if m.Type != types.ModelTypeGeneration {
		return nil, errors.New("ChatGPT plan supports generation models only")
	}
	if s.db == nil {
		return nil, errors.New("ChatGPT authorization database is unavailable")
	}
	manager, policy := s.chatGPT, s.chatGPTPolicy
	if manager == nil {
		manager = chatgpt.NewManager(s.db)
	}
	if policy == nil {
		policy = NewChatGPTPolicyService(s.db)
	}
	accountID := m.Parameters.ExtraConfig["chatgpt_account_id"]
	cfg := chat.ConfigFromModel(m)
	cfg.ChatGPTToken = func(ctx context.Context) (string, error) {
		token, err := manager.AccessToken(ctx, accountID)
		if err != nil {
			return "", &types.ChatGPTDeferredError{Reason: "needs_login", RetryAt: time.Now().Add(5 * time.Minute), Cause: err}
		}
		return token, nil
	}
	cfg.ChatGPTHTTPClient = s.chatGPTHTTPClient
	inner, err := chat.NewChat(cfg, s.ollamaService)
	if err != nil {
		return nil, err
	}
	return &chatGPTBudgetChat{inner: inner, policy: policy, fallback: func(ctx context.Context, id string) (chat.Chat, error) {
		model, err := s.repo.GetByID(ctx, types.SingleSpaceID, id)
		if err != nil {
			return nil, err
		}
		if model == nil || model.Source != types.ModelSourceLocal || model.Type != types.ModelTypeGeneration || model.Status != types.ModelStatusActive {
			return nil, errors.New("configured fallback is not an active local generation model")
		}
		return s.GetChatModel(ctx, id)
	}}, nil
}

type chatGPTBudgetChat struct {
	inner    chat.Chat
	policy   *ChatGPTPolicyService
	fallback func(context.Context, string) (chat.Chat, error)
}

func (c *chatGPTBudgetChat) GetModelName() string { return c.inner.GetModelName() }
func (c *chatGPTBudgetChat) GetModelID() string   { return c.inner.GetModelID() }
func (c *chatGPTBudgetChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	p, err := c.policy.Reserve(ctx)
	if err != nil {
		if p != nil && types.IsChatGPTDeferred(err) && p.FallbackModelID != "" {
			result, fallbackErr := c.useFallback(ctx, p.FallbackModelID, messages, opts)
			if fallbackErr != nil {
				return nil, c.policy.deferred(p, errors.Join(err, fallbackErr))
			}
			return result, nil
		}
		return nil, err
	}
	result, err := c.inner.Chat(ctx, messages, opts)
	if !types.IsChatGPTQuotaError(err) {
		return result, err
	}
	p, pauseErr := c.policy.Pause(ctx, err)
	if pauseErr != nil {
		var upstream *types.ChatGPTError
		errors.As(err, &upstream)
		return nil, &types.ChatGPTDeferredError{Reason: upstream.Code, RetryAt: c.policy.now().Add(5 * time.Minute), Cause: errors.Join(err, pauseErr)}
	}
	if p.FallbackModelID != "" {
		result, fallbackErr := c.useFallback(ctx, p.FallbackModelID, messages, opts)
		if fallbackErr != nil {
			return nil, c.policy.deferred(p, errors.Join(err, fallbackErr))
		}
		return result, nil
	}
	return nil, c.policy.deferred(p, err)
}

func (s *modelService) chatGPTFallbackInUse(ctx context.Context, id string) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&types.ChatGPTPolicy{}).Where("fallback_model_id=?", id).Count(&count).Error
	return count != 0, err
}
func (c *chatGPTBudgetChat) useFallback(ctx context.Context, id string, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	model, err := c.fallback(ctx, id)
	if err != nil {
		return nil, err
	}
	return model.Chat(ctx, messages, opts)
}
func (c *chatGPTBudgetChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	// Buffer until the terminal event so a fallback never mixes partial output
	// from two models. The provider still uses HTTP/SSE for every request.
	result, err := c.Chat(ctx, messages, opts)
	if err != nil {
		return nil, err
	}
	out := make(chan types.StreamResponse, 1)
	out <- types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: result.Content, Done: true, FinishReason: result.FinishReason, Usage: &result.Usage}
	close(out)
	return out, nil
}
