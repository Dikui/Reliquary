// Package errors: parse stage error codes.
//
// These constants are the stable wire format the frontend uses to map a
// failure to a localized "what to do about it" message. Adding a new code
// is non-breaking; renaming an existing one requires a coordinated
// frontend release because the i18n keys are looked up by code.
//
// Codes are SCREAMING_SNAKE so they survive JSON case transforms and look
// distinct from Go identifiers in logs/dashboards.
package errors

const (
	// ErrCodeChunkingFailed — text chunking step itself failed (rare;
	// usually only on extreme-size inputs).
	ErrCodeChunkingFailed = "CHUNKING_FAILED"

	// ErrCodeEmbeddingRateLimit — embedding provider returned 429 or
	// equivalent. Suggest retrying later or scaling out.
	ErrCodeEmbeddingRateLimit = "EMBEDDING_RATE_LIMIT"

	// ErrCodeEmbeddingProviderFail — non-rate-limit embedding error
	// (auth, model not found, bad input). Usually permanent without
	// config changes.
	ErrCodeEmbeddingProviderFail = "EMBEDDING_PROVIDER_FAIL"

	// ErrCodeIndexWriteFailed reports a PostgreSQL index write failure.
	// Keep the serialized code used by ingestion events and existing clients.
	ErrCodeIndexWriteFailed = "VECTORSTORE_WRITE_FAILED"

	// ErrCodeTaskTimeout: queue retry budget exhausted. Used by the
	// dead-letter callback when promoting a task failure into a stage
	// failure for the whole task.
	ErrCodeTaskTimeout = "TASK_TIMEOUT"

	// ErrCodeUnknown — fallback when a wrapped error doesn't classify.
	// The full message is still recorded in error_detail so operators
	// can debug; the UI shows a generic "see admin" hint.
	ErrCodeUnknown = "UNKNOWN"
)
