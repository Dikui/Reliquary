package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	agenttools "reliquary/internal/agent/tools"
	"reliquary/internal/common"
	"reliquary/internal/event"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// toolDisplayNames maps internal tool names to user-friendly display labels.
var toolDisplayNames = map[string]string{
	agenttools.ToolThinking:            "深度思考",
	agenttools.ToolTodoWrite:           "制定计划",
	agenttools.ToolGrepChunks:          "关键词搜索",
	agenttools.ToolKnowledgeSearch:     "知识搜索",
	agenttools.ToolListKnowledgeChunks: "查看文档分块",
	agenttools.ToolGetDocumentInfo:     "获取文档信息",
	agenttools.ToolDataAnalysis:        "数据分析",
	agenttools.ToolDataSchema:          "查看数据结构",
	agenttools.ToolWebFetch:            "获取网页",
}

// formatToolHint returns a concise human-readable hint for a tool call, e.g. `搜索网页("query text")`.
// Uses display names instead of internal tool names, and hides sensitive arguments.
func formatToolHint(name string, args map[string]any) string {
	name = agenttools.CanonicalToolName(name)
	displayName := name
	if dn, ok := toolDisplayNames[name]; ok {
		displayName = dn
	}

	if len(args) == 0 {
		return displayName
	}
	for _, v := range args {
		if s, ok := v.(string); ok {
			if len(s) > 40 {
				s = s[:40] + "…"
			}
			return fmt.Sprintf(`%s("%s")`, displayName, s)
		}
	}
	return displayName
}

// executeToolCalls runs every tool call in the LLM response, appending results to step.ToolCalls.
// It also emits tool-call and tool-result events, and optionally runs reflection after each call.
// When ParallelToolCalls is enabled and there are 2+ tool calls, they execute concurrently.
func (e *AgentEngine) executeToolCalls(
	ctx context.Context, response *types.ChatResponse,
	step *types.AgentStep, iteration int, sessionID, assistantMessageID string,
) {
	if len(response.ToolCalls) == 0 {
		return
	}

	round := iteration + 1
	n := len(response.ToolCalls)

	// A completion-token cap cuts the response mid-serialization, so every call
	// in it may carry incomplete arguments. Execution must wait for complete
	// arguments so queries and other tool inputs retain their intended meaning.
	if isLengthFinishReason(response.FinishReason) {
		logger.Warnf(ctx, "[Agent][Round-%d] Response hit the completion-token cap (finish=%s); "+
			"refusing %d tool call(s) with possibly truncated arguments",
			round, response.FinishReason, n)
		e.failTruncatedToolCalls(ctx, response, step, iteration, sessionID)
		return
	}

	logger.Infof(ctx, "[Agent][Round-%d] Executing %d tool call(s)", round, n)

	// Use parallel execution when enabled and there are multiple tool calls
	if e.config.ParallelToolCalls && n >= 2 {
		e.executeToolCallsParallel(ctx, response, step, iteration, sessionID, assistantMessageID)
		return
	}

	for i, tc := range response.ToolCalls {
		e.executeSingleToolCall(ctx, tc, i, step, iteration, round, sessionID, assistantMessageID)
	}
}

// truncatedArgumentsError is handed to the model instead of a tool result when
// the arguments were cut off. It stays tool-neutral: any tool can be the one
// that got truncated, and naming another tool's fields would send the model
// chasing arguments the failing call does not have.
const truncatedArgumentsError = "Tool call was not executed: the model output was cut off " +
	"before the arguments finished, so they are incomplete rather than wrong. " +
	"Re-issue the call with a complete JSON object. If the payload is large, " +
	"split it across several smaller calls."

// failTruncatedToolCalls records every call in a truncated response as failed
// without running any of them, emitting the same events a real execution would
// so the UI and the transcript stay consistent.
func (e *AgentEngine) failTruncatedToolCalls(
	ctx context.Context, response *types.ChatResponse,
	step *types.AgentStep, iteration int, sessionID string,
) {
	for i, tc := range response.ToolCalls {
		toolCall := types.ToolCall{
			ID:               agenttools.NormalizeToolCallID(tc.ID, tc.Function.Name, i),
			Name:             tc.Function.Name,
			Args:             map[string]any{"_raw": tc.Function.Arguments},
			ProviderMetadata: tc.ProviderMetadata,
			Result:           &types.ToolResult{Success: false, Error: truncatedArgumentsError},
		}
		step.ToolCalls = append(step.ToolCalls, toolCall)
		e.emitToolOutcome(ctx, toolCall, iteration, sessionID)
	}
}

// executeToolCallsParallel runs all tool calls concurrently using errgroup,
// collecting results in original order.
func (e *AgentEngine) executeToolCallsParallel(
	ctx context.Context, response *types.ChatResponse,
	step *types.AgentStep, iteration int, sessionID, assistantMessageID string,
) {
	round := iteration + 1
	n := len(response.ToolCalls)
	logger.Infof(ctx, "[Agent][Round-%d] Parallel execution of %d tool calls", round, n)

	results := make([]types.ToolCall, n)
	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)

	for i, tc := range response.ToolCalls {
		i, tc := i, tc // capture loop vars
		g.Go(func() error {
			toolCall := e.runToolCall(gCtx, tc, i, iteration, round, sessionID, assistantMessageID)
			mu.Lock()
			results[i] = toolCall
			mu.Unlock()
			return nil // best-effort: don't cancel siblings on failure
		})
	}

	_ = g.Wait()

	// Append results and emit events in original order
	for _, toolCall := range results {
		step.ToolCalls = append(step.ToolCalls, toolCall)
		e.emitToolOutcome(ctx, toolCall, iteration, sessionID)
	}
}

// emitToolOutcome publishes the result and action events for one finished tool
// call. Every path that produces a ToolCall goes through here — sequential,
// parallel, and refused-as-truncated — so the UI sees one event shape.
func (e *AgentEngine) emitToolOutcome(
	ctx context.Context, toolCall types.ToolCall, iteration int, sessionID string,
) {
	result := toolCall.Result
	if result == nil {
		result = &types.ToolResult{Success: false, Error: "no result"}
	}

	e.eventBus.Emit(ctx, event.Event{
		ID:        toolCall.ID + "-tool-result",
		Type:      event.EventAgentToolResult,
		SessionID: sessionID,
		Data: event.AgentToolResultData{
			ToolCallID: toolCall.ID,
			ToolName:   toolCall.Name,
			Output:     result.Output,
			Error:      result.Error,
			Success:    result.Success,
			Duration:   toolCall.Duration,
			Iteration:  iteration,
			Data:       agenttools.SanitizeToolDataForPersist(toolCall.Name, result.Data),
		},
	})

	e.eventBus.Emit(ctx, event.Event{
		ID:        toolCall.ID + "-tool-exec",
		Type:      event.EventAgentTool,
		SessionID: sessionID,
		Data: event.AgentActionData{
			Iteration:  iteration,
			ToolName:   toolCall.Name,
			ToolInput:  toolCall.Args,
			ToolOutput: result.Output,
			Success:    result.Success,
			Error:      result.Error,
			Duration:   toolCall.Duration,
		},
	})
}

// executeSingleToolCall runs one tool call sequentially (original behavior).
func (e *AgentEngine) executeSingleToolCall(
	ctx context.Context, tc types.LLMToolCall, i int,
	step *types.AgentStep, iteration, round int, sessionID, assistantMessageID string,
) {
	toolCall := e.runToolCall(ctx, tc, i, iteration, round, sessionID, assistantMessageID)
	step.ToolCalls = append(step.ToolCalls, toolCall)
	e.emitToolOutcome(ctx, toolCall, iteration, sessionID)
}

// runToolCall handles argument parsing, execution, logging, and pipeline events for a single tool call.
// It returns the completed ToolCall struct. Safe to call from multiple goroutines.
func (e *AgentEngine) runToolCall(
	ctx context.Context, tc types.LLMToolCall, i int,
	iteration, round int, sessionID, assistantMessageID string,
) types.ToolCall {
	tc.ID = agenttools.NormalizeToolCallID(tc.ID, tc.Function.Name, i)
	total := "?" // unknown in isolation; callers log the batch size
	toolTag := fmt.Sprintf("[Agent][Round-%d][Tool %s (%d/%s)]",
		round, tc.Function.Name, i+1, total)

	var args map[string]any
	argsStr := tc.Function.Arguments
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		repaired, truncated := agenttools.RepairJSONDetail(argsStr)
		if repairErr := json.Unmarshal([]byte(repaired), &args); repairErr != nil {
			logger.Errorf(ctx, "%s Failed to parse arguments (repair failed): %v", toolTag, err)
			return types.ToolCall{
				ID:               tc.ID,
				Name:             tc.Function.Name,
				Args:             map[string]any{"_raw": argsStr},
				ProviderMetadata: tc.ProviderMetadata,
				Result: &types.ToolResult{
					Success: false,
					Error: fmt.Sprintf(
						"Failed to parse tool arguments: %v", err,
					) + "\n\nIf the JSON looks cut off, the previous round likely hit the output token cap. " +
						"Retry with complete JSON (required fields first) and a smaller payload.\n\n" +
						"[Analyze the error above and try a different approach.]",
				},
			}
		}
		// Closing off an unterminated string or bracket makes the payload
		// parse, but the values inside are still the partial ones the provider
		// managed to emit. Executing that writes half a file or searches half a
		// query, and the tool reports success either way — so refuse instead.
		// This is the belt for streams that break without a finish reason,
		// where the length check in executeToolCalls has nothing to match on.
		if truncated {
			logger.Warnf(ctx, "%s Arguments were cut off mid-emission (%d bytes); refusing to execute",
				toolTag, len(argsStr))
			return types.ToolCall{
				ID:               tc.ID,
				Name:             tc.Function.Name,
				Args:             map[string]any{"_raw": argsStr},
				ProviderMetadata: tc.ProviderMetadata,
				Result:           &types.ToolResult{Success: false, Error: truncatedArgumentsError},
			}
		}
		logger.Warnf(ctx, "%s Repaired malformed JSON arguments", toolTag)
		// The initial model-context pass could not inspect malformed JSON.
		// Decode the repaired payload before execution, while preserving the
		// exact provider payload already stored in tc.ModelArguments.
		decoded := tc
		decoded.ModelArguments = ""
		decoded.Function.Arguments = repaired
		decodedCalls := []types.LLMToolCall{decoded}
		e.modelContext.DecodeToolCalls(decodedCalls)
		tc.Function.Arguments = decodedCalls[0].Function.Arguments
		tc.ArgumentResolution = decodedCalls[0].ArgumentResolution
		tc.UnresolvedHandles = decodedCalls[0].UnresolvedHandles
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return types.ToolCall{
				ID:               tc.ID,
				Name:             tc.Function.Name,
				Args:             map[string]any{"_raw": tc.Function.Arguments},
				ProviderMetadata: tc.ProviderMetadata,
				Result: &types.ToolResult{
					Success: false,
					Error:   fmt.Sprintf("Failed to parse repaired tool arguments: %v", err),
				},
			}
		}
	}

	logger.Debugf(ctx, "%s Args: %s", toolTag, tc.Function.Arguments)

	toolCallStartTime := time.Now()

	// Emit tool hint for UI progress display
	toolHint := formatToolHint(tc.Function.Name, args)
	e.eventBus.Emit(ctx, event.Event{
		ID:        tc.ID + "-tool-hint",
		Type:      event.EventAgentToolCall,
		SessionID: sessionID,
		Data: event.AgentToolCallData{
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			Arguments:  args,
			Iteration:  iteration,
			Hint:       toolHint,
		},
	})

	common.PipelineInfo(ctx, "Agent", "tool_call_start", map[string]interface{}{
		"iteration":    iteration,
		"round":        round,
		"tool":         tc.Function.Name,
		"tool_call_id": tc.ID,
		"tool_index":   fmt.Sprintf("%d/%s", i+1, total),
	})

	toolCtx := ctx

	principal, _ := types.PrincipalFromContext(ctx)
	execTimeout := defaultToolExecTimeout
	toolExecCtx := agenttools.WithToolExecContext(toolCtx, &agenttools.ToolExecContext{
		SessionID:          sessionID,
		AssistantMessageID: assistantMessageID,
		EventBus:           e.eventBus,
		ToolCallID:         tc.ID,
		UserID:             principal.StorageID(),
		// ApprovalCtx keeps the round-level ctx without the per-tool execution timeout,
		// so MCP tool human-approval (issue #1173) can legitimately block longer.
		ApprovalCtx: toolCtx,
		ExecTimeout: execTimeout,
	})

	var result *types.ToolResult
	var err error
	if len(tc.UnresolvedHandles) > 0 {
		// A temporary handle is not an application identity. Fail before tool
		// execution so a hallucinated/stale cN/dN/bN/wN/iN/res:// token can
		// never reach persistence, an external service, or a routing decision.
		err = fmt.Errorf("tool arguments contain unresolved model handles: %v", tc.UnresolvedHandles)
	} else {
		execCtx, toolCancel := context.WithTimeout(toolExecCtx, execTimeout)
		result, err = e.toolRegistry.ExecuteTool(
			execCtx, tc.Function.Name,
			json.RawMessage(tc.Function.Arguments),
		)
		toolCancel()
	}
	duration := time.Since(toolCallStartTime).Milliseconds()

	toolCall := types.ToolCall{
		ID:               tc.ID,
		Name:             tc.Function.Name,
		Args:             args,
		Result:           result,
		Duration:         duration,
		ProviderMetadata: tc.ProviderMetadata,
	}

	if err != nil {
		logger.Errorf(ctx, "%s Failed in %dms: %v", toolTag, duration, err)
		toolCall.Result = &types.ToolResult{
			Success: false,
			Error:   err.Error(),
		}
	} else {
		success := result != nil && result.Success
		outputLen := 0
		if result != nil {
			outputLen = len(result.Output)
		}
		logger.Infof(ctx, "%s Completed in %dms: success=%v, output=%d chars",
			toolTag, duration, success, outputLen)
	}

	// Pipeline event for monitoring
	toolSuccess := toolCall.Result != nil && toolCall.Result.Success
	pipelineFields := map[string]interface{}{
		"iteration":    iteration,
		"round":        round,
		"tool":         tc.Function.Name,
		"tool_call_id": tc.ID,
		"duration_ms":  duration,
		"success":      toolSuccess,
	}
	if toolCall.Result != nil && toolCall.Result.Error != "" {
		pipelineFields["error"] = toolCall.Result.Error
	}
	if err != nil {
		common.PipelineError(ctx, "Agent", "tool_call_result", pipelineFields)
	} else if toolSuccess {
		common.PipelineInfo(ctx, "Agent", "tool_call_result", pipelineFields)
	} else {
		common.PipelineWarn(ctx, "Agent", "tool_call_result", pipelineFields)
	}

	if toolCall.Result != nil && toolCall.Result.Output != "" {
		preview := toolCall.Result.Output
		if len(preview) > 500 {
			preview = preview[:500] + "... (truncated)"
		}
		logger.Debugf(ctx, "%s Output preview:\n%s", toolTag, preview)
	}
	if toolCall.Result != nil && toolCall.Result.Error != "" {
		logger.Debugf(ctx, "%s Tool error: %s", toolTag, toolCall.Result.Error)
	}

	return toolCall
}
