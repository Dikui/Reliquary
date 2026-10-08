package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"reliquary/internal/agent/compaction"
	"reliquary/internal/types"
)

const (
	// DefaultAgentTemperature is the default temperature for the agent
	DefaultAgentTemperature = 0.7
	// DefaultAgentMaxIterations is the default maximum number of iterations for the agent
	DefaultAgentMaxIterations = 20
	// DefaultUseCustomSystemPrompt is the default whether to use custom system prompt for the agent
	DefaultUseCustomSystemPrompt = false

	// defaultLLMStallTimeout is how long a single LLM stream may produce no
	// output before it is cancelled. It is a stall budget, not a total budget:
	// a round that streams a large data_analysis query
	// runs for minutes while emitting continuously, and killing it on elapsed
	// time throws away the half-assembled call. The overall ceiling belongs to
	// the provider transport (RELIQUARY_LLM_STREAM_TIMEOUT_SECONDS).
	// Can be overridden via AgentConfig.LLMCallTimeout.
	defaultLLMStallTimeout = 120 * time.Second

	// defaultToolExecTimeout is the default maximum time for a single tool execution.
	// Prevents long-running tools (web_fetch, database_query) from hanging indefinitely.
	defaultToolExecTimeout = 60 * time.Second

	// maxLLMRetries is the maximum number of retries for transient LLM errors.
	maxLLMRetries = 2

	// maxEmptyResponseRetries is the maximum number of retries when the LLM
	// returns an empty content with a natural stop (no tool calls). This guards
	// against the agent completing with an empty answer when the LLM fails to
	// produce content (e.g., thinking-only loops without KB).
	// Trade-off: each retry costs ~2s of LLM latency; 2 retries = max 4s extra.
	maxEmptyResponseRetries = 2

	// maxRepeatedResponseRounds is the maximum number of consecutive rounds
	// where the LLM returns identical content without any tool calls before
	// the loop is forcibly terminated. This catches stuck loops caused by
	// unhandled finish reasons (e.g., content_filter not caught elsewhere).
	maxRepeatedResponseRounds = 2
)

// transientErrorMarkers are substrings that indicate a transient (retryable) error.
var transientErrorMarkers = []string{
	"429", "rate limit",
	"500", "502", "503", "504",
	"overloaded", "timeout", "timed out",
	"connection", "server error", "temporarily unavailable",
	// A broken or silent stream is worth one more attempt: the round produced
	// no usable turn, and the alternative is ending the conversation on a
	// partial response.
	"deadline exceeded", "stalled",
}

// isTransientError checks whether an error is likely transient and worth retrying.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	for _, marker := range transientErrorMarkers {
		if strings.Contains(errStr, marker) {
			return true
		}
	}
	return false
}

// getLLMStallTimeout returns how long an LLM stream may go silent before it is
// cancelled, from AgentConfig.LLMCallTimeout or the default.
func (e *AgentEngine) getLLMStallTimeout() time.Duration {
	if e.config.LLMCallTimeout > 0 {
		return time.Duration(e.config.LLMCallTimeout) * time.Second
	}
	return defaultLLMStallTimeout
}

// contextSafetyTokens is slack between what we estimate the request costs and
// what the provider will actually count. Token estimation is approximate and
// providers add their own scaffolding; without a margin the request that
// exactly fits by our arithmetic is the one that gets rejected.
const contextSafetyTokens = 4096

// getCompletionTokenBudget is the max_tokens / max_completion_tokens sent on
// each ReAct LLM round. An unset budget defaults to 4096.
func (e *AgentEngine) getCompletionTokenBudget() int {
	configured := 0
	if e.config != nil {
		configured = e.config.MaxCompletionTokens
	}
	return types.AgentRoundMaxCompletionTokens(configured)
}

// contextReserveTokens is the part of the window that history may not occupy,
// because the next response has to fit there. Sizing it from the round's own
// completion budget is what a fixed reserve gets wrong: an agent allowed to
// emit 24576 tokens needs at least that much free, or the request is accepted
// and the reply is truncated.
func (e *AgentEngine) contextReserveTokens() int {
	return max(e.getCompletionTokenBudget()+contextSafetyTokens, compaction.DefaultReserveTokens)
}

// clampCompletionBudgetToContext shrinks the round's completion budget to what
// is actually left in the window. Asking for more output than the window can
// hold is a request the provider rejects outright, which reads to the agent as
// an unexplained failure.
func (e *AgentEngine) clampCompletionBudgetToContext(currentTokens int) int {
	budget := e.getCompletionTokenBudget()
	if e.config == nil || e.config.MaxContextTokens <= 0 {
		return budget
	}
	available := e.config.MaxContextTokens - currentTokens - contextSafetyTokens
	return max(min(budget, available), 1)
}

// generateEventID generates a unique event ID with type suffix for better traceability
func generateEventID(suffix string) string {
	return fmt.Sprintf("%s-%s", uuid.New().String()[:8], suffix)
}
