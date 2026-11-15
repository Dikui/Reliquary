package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"reliquary/internal/chatgpt"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type AgentModelService struct {
	models   interfaces.ModelService
	accounts *chatgpt.Manager
	policy   *ChatGPTPolicyService
	client   *http.Client
}

func NewAgentModelService(models interfaces.ModelService, accounts *chatgpt.Manager, policy *ChatGPTPolicyService) *AgentModelService {
	return &AgentModelService{models: models, accounts: accounts, policy: policy}
}

// Respond keeps OAuth credentials on the server and leaves tool execution to Pi.
func (s *AgentModelService) Respond(ctx context.Context, id string, body []byte, emit func([]byte) error) error {
	model, err := s.models.GetModelByID(ctx, id)
	if err != nil {
		return err
	}
	if model.Source != types.ModelSourceChatGPT || model.Type != types.ModelTypeGeneration || model.Status != types.ModelStatusActive {
		return fmt.Errorf("select an active ChatGPT generation model")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	if accounting, ok := s.models.(interface {
		ModelCallAccounting(*types.Model) *types.ModelCallAccounting
	}); ok {
		ctx = types.WithModelCallAccounting(ctx, accounting.ModelCallAccounting(model))
	}
	client, err := chat.NewChatGPTChat(&chat.ChatConfig{ModelName: model.Name, ModelID: model.ID, ChatGPTHTTPClient: s.client,
		ChatGPTToken: func(ctx context.Context) (string, error) {
			token, err := s.accounts.AccessToken(ctx, model.Parameters.ExtraConfig["chatgpt_account_id"])
			if err != nil {
				return "", &types.ChatGPTError{Kind: "authentication", Code: "needs_login", Message: "请在模型设置重新授权"}
			}
			return token, nil
		}})
	if err != nil {
		return err
	}
	if _, err := s.policy.Reserve(ctx); err != nil {
		return err
	}
	response, err := client.OpenAgentResponse(ctx, body)
	if err != nil {
		return s.recordFailure(ctx, err)
	}
	defer response.Body.Close()
	err = relayAgentResponse(response.Body, emit)
	return s.recordFailure(ctx, err)
}

func (s *AgentModelService) recordFailure(ctx context.Context, err error) error {
	if types.IsChatGPTQuotaError(err) {
		_, pauseErr := s.policy.Pause(ctx, err)
		return errors.Join(err, pauseErr)
	}
	return err
}

func relayAgentResponse(reader io.Reader, emit func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var lines, data []string
	size := 0
	process := func() (bool, error) {
		if len(lines) == 0 {
			return false, nil
		}
		var event struct {
			Type     string `json:"type"`
			Response struct {
				Status string `json:"status"`
				Error  *struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"response"`
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
			Code string `json:"code"`
		}
		if len(data) > 0 {
			if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &event); err != nil {
				return false, fmt.Errorf("invalid model stream event")
			}
		}
		switch event.Type {
		case "response.completed":
			if event.Response.Status != "completed" || event.Response.Error != nil {
				return false, fmt.Errorf("invalid completed response")
			}
			return true, emit([]byte(strings.Join(lines, "\n") + "\n\n"))
		case "response.failed", "response.incomplete", "error":
			code := event.Code
			if event.Error != nil {
				code = event.Error.Code
			}
			if event.Response.Error != nil {
				code = event.Response.Error.Code
			}
			return false, &types.ChatGPTError{Kind: "stream", Code: code, Message: "model response did not complete"}
		}
		return false, emit([]byte(strings.Join(lines, "\n") + "\n\n"))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			complete, err := process()
			if complete || err != nil {
				return err
			}
			lines, data = nil, nil
			size = 0
			continue
		}
		lines = append(lines, line)
		size += len(line)
		if size > 8*1024*1024 {
			return fmt.Errorf("model stream event is too large")
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	complete, err := process()
	if err != nil {
		return err
	}
	if !complete {
		return io.ErrUnexpectedEOF
	}
	return nil
}
