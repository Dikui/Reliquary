package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OpenAgentResponse preserves Pi's transcript, including function results and
// encrypted reasoning, while admitting only the documented local tool types.
func (c *ChatGPTChat) OpenAgentResponse(ctx context.Context, body []byte) (*http.Response, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil || request == nil {
		return nil, fmt.Errorf("invalid agent request")
	}
	allowed := map[string]bool{"model": true, "input": true, "instructions": true, "tools": true,
		"tool_choice": true, "parallel_tool_calls": true, "reasoning": true, "include": true,
		"store": true, "stream": true, "prompt_cache_key": true}
	for key := range request {
		if !allowed[key] {
			return nil, fmt.Errorf("unsupported agent field %s", key)
		}
	}
	var input []map[string]json.RawMessage
	if err := json.Unmarshal(request["input"], &input); err != nil || len(input) == 0 {
		return nil, fmt.Errorf("agent input must be a nonempty array")
	}
	for _, item := range input {
		var kind, role string
		_ = json.Unmarshal(item["type"], &kind)
		_ = json.Unmarshal(item["role"], &role)
		if role == "system" {
			return nil, fmt.Errorf("use instructions or developer messages")
		}
		switch kind {
		case "", "message", "reasoning", "function_call", "function_call_output":
		case "additional_tools":
			if err := validateAgentTools(item["tools"], false); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported agent input type %s", kind)
		}
	}
	if err := validateAgentTools(request["tools"], true); err != nil {
		return nil, err
	}
	request["model"], _ = json.Marshal(c.modelName)
	request["store"] = json.RawMessage("false")
	request["stream"] = json.RawMessage("true")
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return c.openBody(ctx, raw)
}

func validateAgentTools(raw json.RawMessage, namespaces bool) error {
	if len(raw) == 0 {
		return nil
	}
	var tools []struct {
		Type  string          `json:"type"`
		Tools json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		return fmt.Errorf("invalid agent tools")
	}
	for _, tool := range tools {
		switch {
		case tool.Type == "function":
		case namespaces && tool.Type == "namespace":
			if err := validateAgentTools(tool.Tools, false); err != nil {
				return err
			}
		default:
			return fmt.Errorf("only local function tools are supported")
		}
	}
	return nil
}
