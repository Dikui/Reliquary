package types

import (
	"context"
	"os"
	"strings"
)

// EnvLanguage returns the RELIQUARY_LANGUAGE environment variable value, or empty string if unset.
func EnvLanguage() string {
	return strings.TrimSpace(os.Getenv("RELIQUARY_LANGUAGE"))
}

// DefaultLanguage returns the configured default language locale.
// It reads the RELIQUARY_LANGUAGE environment variable; if unset, falls back to "zh-CN".
func DefaultLanguage() string {
	if lang := EnvLanguage(); lang != "" {
		return lang
	}
	return "zh-CN"
}

// TenantIDFromContext extracts the tenant ID from ctx.
// Returns (0, false) when the key is absent or the value is not uint64.
func TenantIDFromContext(ctx context.Context) (uint64, bool) {
	v, ok := ctx.Value(TenantIDContextKey).(uint64)
	return v, ok
}

// MustTenantIDFromContext extracts the tenant ID from ctx, panicking if missing.
func MustTenantIDFromContext(ctx context.Context) uint64 {
	v, ok := TenantIDFromContext(ctx)
	if !ok {
		panic("types.TenantIDContextKey not set in context")
	}
	return v
}

// TenantInfoFromContext extracts the *Tenant from ctx.
func TenantInfoFromContext(ctx context.Context) (*Tenant, bool) {
	v, ok := ctx.Value(TenantInfoContextKey).(*Tenant)
	return v, ok && v != nil
}

// RequestIDFromContext extracts the request ID string from ctx.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(RequestIDContextKey).(string)
	return v, ok && v != ""
}

// UserIDFromContext extracts the user ID string from ctx.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(UserIDContextKey).(string)
	return v, ok && v != ""
}

// TaskInitiator is the authenticated caller that submitted an asynchronous
// task. Workers restore this identity into their execution context.
type TaskInitiator struct {
	UserID string `json:"user_id,omitempty"`
}

// TaskInitiatorFromContext snapshots the authenticated user for a task payload.
func TaskInitiatorFromContext(ctx context.Context) TaskInitiator {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return TaskInitiator{}
	}
	return TaskInitiator{UserID: userID}
}

// Apply restores a captured task initiator onto a worker context. Empty or
// legacy payloads are a no-op and therefore retain the system-task fallback.
func (i TaskInitiator) Apply(ctx context.Context) context.Context {
	if i.UserID == "" {
		return ctx
	}
	ctx = context.WithValue(ctx, UserIDContextKey, i.UserID)

	return ctx
}

func SessionTenantIDFromContext(ctx context.Context) (uint64, bool) { return TenantIDFromContext(ctx) }

// WithSessionID carries the active conversation ID.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, SessionIDContextKey, sessionID)
}

// SessionIDFromContext extracts the current session ID from ctx.
// Returns ("", false) when the key is absent or the value is not a non-empty string.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(SessionIDContextKey).(string)
	return v, ok && v != ""
}

// WithBackgroundTask marks ctx as originating from an asynq background worker
// (document parse / summary / question / context / multimodal enrichment). The
// chat concurrency governor throttles only background LLM traffic, so this flag
// determines whether the per-model concurrency limit applies. See
// BackgroundTaskContextKey.
func WithBackgroundTask(ctx context.Context) context.Context {
	return context.WithValue(ctx, BackgroundTaskContextKey, true)
}

// IsBackgroundTask reports whether ctx was marked as a background worker task
// (see WithBackgroundTask). Returns false for interactive / HTTP request paths.
func IsBackgroundTask(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(BackgroundTaskContextKey).(bool)
	return v
}

type taskRetryMetadata struct {
	retried  int
	maxRetry int
}

// WithTaskRetryMetadata records retry counters for task executors that do not
// provide Asynq's native worker context, notably direct test handlers.
func WithTaskRetryMetadata(ctx context.Context, retried, maxRetry int) context.Context {
	return context.WithValue(ctx, taskRetryMetadataContextKey{}, taskRetryMetadata{
		retried: retried, maxRetry: maxRetry,
	})
}

// TaskRetryMetadataFromContext returns retry counters supplied by a non-Asynq
// task executor. The boolean is false for ordinary request contexts.
func TaskRetryMetadataFromContext(ctx context.Context) (retried, maxRetry int, ok bool) {
	if ctx == nil {
		return 0, 0, false
	}
	metadata, ok := ctx.Value(taskRetryMetadataContextKey{}).(taskRetryMetadata)
	if !ok {
		return 0, 0, false
	}
	return metadata.retried, metadata.maxRetry, true
}

type taskRetryMetadataContextKey struct{}

// WithLLMCallMetadata annotates a provider call for cache observability. The
// fingerprint must be a hash, never raw prompt content.
func WithLLMCallMetadata(ctx context.Context, purpose, prefixFingerprint string) context.Context {
	if strings.TrimSpace(purpose) != "" {
		ctx = context.WithValue(ctx, LLMCallPurposeContextKey, strings.TrimSpace(purpose))
	}
	if strings.TrimSpace(prefixFingerprint) != "" {
		ctx = context.WithValue(ctx, LLMPromptPrefixFingerprintContextKey, strings.TrimSpace(prefixFingerprint))
	}
	return ctx
}

// LLMCallMetadataFromContext returns the cache-observability labels attached
// by the orchestration layer.
func LLMCallMetadataFromContext(ctx context.Context) (purpose, prefixFingerprint string) {
	if ctx == nil {
		return "", ""
	}
	purpose, _ = ctx.Value(LLMCallPurposeContextKey).(string)
	prefixFingerprint, _ = ctx.Value(LLMPromptPrefixFingerprintContextKey).(string)
	return purpose, prefixFingerprint
}

// LanguageFromContext extracts the language locale string from ctx (e.g. "zh-CN", "en-US").
// Returns ("zh-CN", false) when the key is absent.
func LanguageFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(LanguageContextKey).(string)
	return v, ok && v != ""
}

// ResolveLanguage resolves the effective locale for work that may carry its own
// language, in descending precedence: the explicit locale, the locale in ctx,
// then DefaultLanguage().
//
// Async workers must use this rather than reading a payload field directly: a
// task payload persisted before the language field existed, or enqueued from a
// background path that never passed through the HTTP language middleware,
// carries an empty locale. Interpolating that empty value into a prompt yields
// instructions like "Write in ." and lets the model pick a language at random.
func ResolveLanguage(ctx context.Context, locale string) string {
	if locale = strings.TrimSpace(locale); locale != "" {
		return locale
	}
	if ctxLocale, ok := LanguageFromContext(ctx); ok {
		return ctxLocale
	}
	return DefaultLanguage()
}

// ResolveLanguageName is ResolveLanguage rendered as the human-readable name
// that prompt templates interpolate (e.g. "Chinese (Simplified)").
//
// It is idempotent over already-resolved names: LanguageLocaleName passes
// unknown values through, so re-resolving a display name returns it unchanged.
func ResolveLanguageName(ctx context.Context, locale string) string {
	return LanguageLocaleName(ResolveLanguage(ctx, locale))
}

// LanguageFromContextOrDefault returns the locale carried by ctx, falling back
// to DefaultLanguage(). Use it when persisting a locale onto an async task
// payload so downstream workers never inherit an empty language.
func LanguageFromContextOrDefault(ctx context.Context) string {
	return ResolveLanguage(ctx, "")
}

// LanguageNameFromContext returns the human-readable language name for use in prompts.
// e.g. "zh-CN" -> "Chinese (Simplified)", "en-US" -> "English", "ko-KR" -> "Korean"
// Falls back to DefaultLanguage() (RELIQUARY_LANGUAGE env, then "zh-CN").
func LanguageNameFromContext(ctx context.Context) string {
	return ResolveLanguageName(ctx, "")
}

// LanguageLocaleName maps a locale code to a human-readable language name for LLM prompts.
func LanguageLocaleName(locale string) string {
	switch locale {
	case "zh-CN", "zh", "zh-Hans":
		return "Chinese (Simplified)"
	case "zh-TW", "zh-HK", "zh-Hant":
		return "Chinese (Traditional)"
	case "en-US", "en", "en-GB":
		return "English"
	case "ko-KR", "ko":
		return "Korean"
	case "ja-JP", "ja":
		return "Japanese"
	case "ru-RU", "ru":
		return "Russian"
	case "fr-FR", "fr":
		return "French"
	case "de-DE", "de":
		return "German"
	case "es-ES", "es":
		return "Spanish"
	case "pt-BR", "pt":
		return "Portuguese"
	default:
		// For unknown locales, return the locale itself
		return locale
	}
}

// IsAuthenticatedFromContext reports whether a user identity was verified.
func IsAuthenticatedFromContext(ctx context.Context) bool {
	id, ok := UserIDFromContext(ctx)
	return ok && strings.TrimSpace(id) != ""
}
