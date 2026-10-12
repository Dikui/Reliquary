package types

// ContextKey defines a type for context keys to avoid string collision
type ContextKey string

const (
	// SpaceIDContextKey is the context key for space ID
	SpaceIDContextKey ContextKey = "SpaceID"
	// SpaceInfoContextKey is the context key for space information
	SpaceInfoContextKey ContextKey = "SpaceInfo"
	// RequestIDContextKey is the context key for request ID
	RequestIDContextKey ContextKey = "RequestID"
	// LoggerContextKey is the context key for logger
	LoggerContextKey ContextKey = "Logger"
	// UserContextKey is the context key for user information
	UserContextKey ContextKey = "User"
	// UserIDContextKey is the context key for user ID
	UserIDContextKey ContextKey = "UserID"
	// PrincipalContextKey is the context key for the terminal caller principal.
	PrincipalContextKey ContextKey = "Principal"
	// EmbedQueryContextKey is the context key for embedding query text
	EmbedQueryContextKey ContextKey = "EmbedQuery"
	// LanguageContextKey is the context key for user language preference (e.g. "zh-CN", "en-US")
	LanguageContextKey ContextKey = "Language"
	// context. It is declared here so context_clone.go can name every context key
	// without importing the middleware that authenticates it.
	// BackgroundTaskContextKey marks a context whose model calls originate from
	// an queue background worker (document parse / summary / question / context /
	// multimodal enrichment) rather than a user-facing HTTP request. The model
	// concurrency governor uses this to throttle only background LLM traffic,
	// leaving foreground model latency untouched. See WithBackgroundTask.
	BackgroundTaskContextKey ContextKey = "BackgroundTask"
	// LLMCallPurposeContextKey labels the product-level reason for a model
	// request (for example article_summary).
	LLMCallPurposeContextKey ContextKey = "LLMCallPurpose"
	// LLMPromptPrefixFingerprintContextKey carries a non-sensitive hash of the
	// intended reusable prompt prefix for cache diagnostics.
	LLMPromptPrefixFingerprintContextKey ContextKey = "LLMPromptPrefixFingerprint"
)

// String returns the string representation of the context key
func (c ContextKey) String() string {
	return string(c)
}
