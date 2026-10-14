package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"reliquary/internal/modelcalls"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

const chatGPTResponsesEndpoint = "https://api.openai.com/v1/responses"

// ChatGPTChat uses the public Responses route with Reliquary's own OAuth
// session. Every call is streamed upstream, including the Chat interface.
type ChatGPTChat struct {
	modelName string
	modelID   string
	endpoint  string
	token     func(context.Context) (string, error)
	client    *http.Client
}

type chatGPTInputPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type chatGPTInput struct {
	Role    string             `json:"role"`
	Content []chatGPTInputPart `json:"content"`
}

// Keeping a closed request type prevents provider-specific ChatOptions fields
// from entering this more restricted subscription route.
type chatGPTRequest struct {
	Model        string         `json:"model"`
	Input        []chatGPTInput `json:"input"`
	Instructions string         `json:"instructions,omitempty"`
	Store        bool           `json:"store"`
	Stream       bool           `json:"stream"`
}

type chatGPTAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
}

type chatGPTResponse struct {
	ID     string           `json:"id"`
	Status string           `json:"status"`
	Error  *chatGPTAPIError `json:"error"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
		InputDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

type chatGPTEvent struct {
	Type     string           `json:"type"`
	Delta    string           `json:"delta"`
	Code     string           `json:"code"`
	Message  string           `json:"message"`
	Param    string           `json:"param"`
	Error    *chatGPTAPIError `json:"error"`
	Response *chatGPTResponse `json:"response"`
}

func NewChatGPTChat(config *ChatConfig) (*ChatGPTChat, error) {
	if config == nil || config.ChatGPTToken == nil {
		return nil, fmt.Errorf("ChatGPT sign-in is required")
	}
	if strings.TrimSpace(config.ModelName) == "" {
		return nil, fmt.Errorf("ChatGPT model selection is required")
	}
	client := config.ChatGPTHTTPClient
	if client == nil {
		client = rawHTTPClient
	}
	// Bearer credentials stay on the documented endpoint. Admission redirects
	// are surfaced as HTTP failures instead of forwarding OAuth credentials.
	clientCopy := *modelcalls.WrapClient(client)
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &ChatGPTChat{
		modelName: config.ModelName, modelID: config.ModelID,
		endpoint: chatGPTResponsesEndpoint, token: config.ChatGPTToken, client: &clientCopy,
	}, nil
}

func (c *ChatGPTChat) GetModelName() string { return c.modelName }
func (c *ChatGPTChat) GetModelID() string   { return c.modelID }

func (c *ChatGPTChat) buildRequest(messages []Message, opts *ChatOptions) (*chatGPTRequest, error) {
	if opts != nil && len(opts.Tools) > 0 {
		return nil, &types.ChatGPTError{Kind: "unsupported", Code: "subscription_sharing_unsupported_capability", Param: "tools", Message: "ChatGPT model source supports generation without tools"}
	}
	req := &chatGPTRequest{Model: c.modelName, Input: make([]chatGPTInput, 0, len(messages)), Store: false, Stream: true}
	var instructions []string
	for _, msg := range messages {
		if len(msg.ToolCalls) > 0 || msg.Role == "tool" {
			return nil, &types.ChatGPTError{Kind: "unsupported", Code: "subscription_sharing_unsupported_capability", Param: "input", Message: "ChatGPT model source does not accept tool messages"}
		}
		if msg.Role == "system" {
			text := msg.Content
			for _, part := range msg.MultiContent {
				if part.Type != "text" {
					return nil, fmt.Errorf("ChatGPT instructions must contain text only")
				}
				text += part.Text
			}
			if len(msg.Images) > 0 {
				return nil, fmt.Errorf("ChatGPT instructions must contain text only")
			}
			instructions = append(instructions, text)
			continue
		}
		if msg.Role != "user" && msg.Role != "assistant" && msg.Role != "developer" {
			return nil, fmt.Errorf("unsupported ChatGPT message role: %s", msg.Role)
		}
		input := chatGPTInput{Role: msg.Role, Content: make([]chatGPTInputPart, 0, 1+len(msg.MultiContent)+len(msg.Images))}
		if msg.Content != "" || (len(msg.MultiContent) == 0 && len(msg.Images) == 0) {
			input.Content = append(input.Content, chatGPTInputPart{Type: "input_text", Text: msg.Content})
		}
		for _, part := range msg.MultiContent {
			switch part.Type {
			case "text":
				input.Content = append(input.Content, chatGPTInputPart{Type: "input_text", Text: part.Text})
			case "image_url":
				if part.ImageURL == nil || part.ImageURL.URL == "" {
					return nil, fmt.Errorf("ChatGPT image input URL is required")
				}
				input.Content = append(input.Content, chatGPTInputPart{Type: "input_image", ImageURL: resolveImageURLForLLM(part.ImageURL.URL), Detail: part.ImageURL.Detail})
			default:
				return nil, fmt.Errorf("unsupported ChatGPT content part: %s", part.Type)
			}
		}
		for _, image := range msg.Images {
			input.Content = append(input.Content, chatGPTInputPart{Type: "input_image", ImageURL: resolveImageURLForLLM(image)})
		}
		req.Input = append(req.Input, input)
	}
	req.Instructions = strings.Join(instructions, "\n\n")
	return req, nil
}

func (c *ChatGPTChat) open(ctx context.Context, messages []Message, opts *ChatOptions) (*http.Response, error) {
	request, err := c.buildRequest(messages, opts)
	if err != nil {
		return nil, err
	}
	if err := secutils.ValidateURLForSSRF(c.endpoint); err != nil {
		return nil, fmt.Errorf("ChatGPT endpoint SSRF check failed: %w", err)
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("ChatGPT sign-in is required")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal ChatGPT request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ChatGPT request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &types.ChatGPTError{Kind: "transport", Message: "ChatGPT request could not be sent", Cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		apiErr := &types.ChatGPTError{Kind: "http", Status: resp.StatusCode, RequestID: resp.Header.Get("x-request-id"), Body: string(body), Message: http.StatusText(resp.StatusCode), Cause: readErr}
		var envelope struct {
			Error  *chatGPTAPIError `json:"error"`
			Detail string           `json:"detail"`
		}
		if json.Unmarshal(body, &envelope) == nil {
			if envelope.Error != nil {
				apiErr.Code, apiErr.Param, apiErr.Message = envelope.Error.Code, envelope.Error.Param, envelope.Error.Message
			} else if envelope.Detail != "" {
				apiErr.Message = envelope.Detail
			}
		}
		return nil, apiErr
	}
	return resp, nil
}

func (c *ChatGPTChat) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error) {
	ctx, cancel := withLLMTimeout(ctx, defaultChatTimeout)
	defer cancel()
	resp, err := c.open(ctx, messages, opts)
	if err != nil {
		return nil, err
	}
	return c.consume(ctx, resp, nil)
}

func (c *ChatGPTChat) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan types.StreamResponse, error) {
	ctx, cancel := withLLMTimeout(ctx, defaultStreamTimeout)
	resp, err := c.open(ctx, messages, opts)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan types.StreamResponse)
	go func() {
		defer cancel()
		defer close(out)
		_, err := c.consume(ctx, resp, func(chunk types.StreamResponse) bool {
			select {
			case out <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		})
		if err != nil {
			select {
			case out <- types.StreamResponse{ResponseType: types.ResponseTypeError, Done: true, Content: err.Error(), FinishReason: types.FinishReasonIncomplete, Data: map[string]interface{}{"error": err}}:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}

func (c *ChatGPTChat) consume(ctx context.Context, resp *http.Response, emit func(types.StreamResponse) bool) (*types.ChatResponse, error) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var data, eventName string
	var content strings.Builder
	result := &types.ChatResponse{}
	requestID := resp.Header.Get("x-request-id")
	failure := func(kind, message string, cause error) *types.ChatGPTError {
		return &types.ChatGPTError{Kind: kind, Status: resp.StatusCode, RequestID: requestID, Message: message, Cause: cause}
	}
	process := func() (bool, error) {
		if data == "" {
			return false, nil
		}
		if data == "[DONE]" {
			return false, failure("interrupted", "ChatGPT stream ended without response.completed", io.ErrUnexpectedEOF)
		}
		var event chatGPTEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return false, failure("invalid_event", "Invalid ChatGPT stream event", err)
		}
		if event.Type == "" {
			event.Type = eventName
		}
		switch event.Type {
		case "response.output_text.delta":
			content.WriteString(event.Delta)
			if emit != nil && event.Delta != "" && !emit(types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: event.Delta}) {
				return false, failure("interrupted", "ChatGPT stream cancelled", ctx.Err())
			}
		case "response.completed":
			if event.Response == nil || event.Response.Error != nil || (event.Response.Status != "" && event.Response.Status != "completed") {
				return false, failure("invalid_event", "Invalid ChatGPT response.completed event", nil)
			}
			final := event.Response.outputText()
			result.Content = content.String()
			if final != "" {
				result.Content = final
			}
			result.FinishReason = "stop"
			result.Usage = event.Response.tokenUsage()
			if emit != nil {
				lastContent := ""
				if content.Len() == 0 {
					lastContent = result.Content
				}
				if !emit(types.StreamResponse{ID: event.Response.ID, ResponseType: types.ResponseTypeAnswer, Content: lastContent, Done: true, FinishReason: "stop", Usage: &result.Usage}) {
					return false, failure("interrupted", "ChatGPT stream cancelled", ctx.Err())
				}
			}
			logUsage(ctx, c.modelName, &result.Usage)
			return true, nil
		case "response.failed", "response.incomplete", "error":
			kind := strings.TrimPrefix(event.Type, "response.")
			apiErr := failure(kind, "ChatGPT "+event.Type, nil)
			apiErr.Body = data
			detail := event.Error
			if event.Response != nil && event.Response.Error != nil {
				detail = event.Response.Error
			}
			if detail != nil {
				apiErr.Code, apiErr.Param, apiErr.Message = detail.Code, detail.Param, detail.Message
			} else {
				apiErr.Code, apiErr.Param = event.Code, event.Param
				if event.Message != "" {
					apiErr.Message = event.Message
				}
			}
			if event.Response != nil && event.Response.IncompleteDetails != nil {
				apiErr.Code = event.Response.IncompleteDetails.Reason
			}
			return false, apiErr
		}
		return false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			done, err := process()
			if err != nil {
				return nil, err
			}
			if done {
				return result, nil
			}
			data, eventName = "", ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			if data != "" {
				data += "\n"
			}
			data += strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			if len(data) > 8*1024*1024 {
				return nil, failure("invalid_event", "ChatGPT stream event exceeds the size limit", nil)
			}
		}
	}
	if scanner.Err() != nil {
		return nil, failure("interrupted", "ChatGPT stream interrupted before response.completed", scanner.Err())
	}
	// SSE permits the final event to end at EOF without a blank separator.
	if done, err := process(); err != nil {
		return nil, err
	} else if done {
		return result, nil
	}
	return nil, failure("interrupted", "ChatGPT stream ended without response.completed", io.ErrUnexpectedEOF)
}

func (r *chatGPTResponse) outputText() string {
	var text strings.Builder
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				text.WriteString(part.Text)
			}
		}
	}
	return text.String()
}

func (r *chatGPTResponse) tokenUsage() types.TokenUsage {
	usage := types.TokenUsage{}
	if r.Usage == nil {
		usage.MarkPromptCacheUnsupported()
		return usage
	}
	usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens = r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.TotalTokens
	if r.Usage.InputDetails != nil {
		cached := r.Usage.InputDetails.CachedTokens
		usage.SetPromptCacheUsage(cached, 0, max(0, usage.PromptTokens-cached), true)
	} else {
		usage.SetPromptCacheUsage(0, 0, usage.PromptTokens, false)
	}
	return usage
}
