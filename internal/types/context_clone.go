package types

import "sort"

// contextCloneAcrossDetach answers one question for every context key in the
// codebase: when a request's context is detached for background work by
// logger.CloneContext, does this key travel with it?
//
// It is an exhaustive decision table rather than a list of keys to keep,
// because the two ways of getting this wrong fail in opposite directions and
// neither default is safe. Dropping a key that carries a RESTRICTION fails
// open: losing a caller's workspace identity lets background work run in
// a scope different from the request. Keeping a key that
// carries a GRANT fails open the other way: a per-request access decision or a
// suppressed-audit marker that outlives its request widens what the background
// work may do. Since no default is universally right, every key states its
// own answer here, next to where such keys are declared, and
// TestEveryContextKeyDeclaresACloneDecision fails when a new key arrives
// without one instead of letting it opt out by silence.
//
// false is a valid, meaningful answer — it means "deliberately does not
// survive a detach" — and is not the same as being absent.
var contextCloneAcrossDetach = map[ContextKey]bool{
	// Caller identity and workspace scope. Background work runs as the same
	// principal in the same workspace, so all of this has to survive; a
	// detached goroutine that loses its space reads another space's rows or
	// none at all.
	SpaceIDContextKey:   true,
	SpaceInfoContextKey: true,
	UserContextKey:      true,
	UserIDContextKey:    true,
	PrincipalContextKey: true,

	// The visitor identity must remain distinct across background work.
	EmbedQueryContextKey: true,

	// Diagnostics and presentation that background work is expected to keep.
	LoggerContextKey:    true,
	RequestIDContextKey: true,
	LanguageContextKey:  true,

	// Marks model calls as coming from an queue worker so the per-model
	// concurrency governor throttles them, leaving foreground model latency
	// alone. Document ingestion detaches mid-flight — knowledge_create hands
	// processChunks to a goroutine on a cloned context, and processChunks is
	// what vectorises every chunk. Dropping the mark there would let exactly
	// the ingestion storm the governor exists to contain run past it. Keeping
	// it can only over-throttle a context that is background by definition.
	BackgroundTaskContextKey: true,

	// ---- Deliberately does not survive a detach ----
	//
	// false here is a decision, not an oversight: each of these describes one
	// request, one call or one attachment rather than the caller, so carrying
	// it into detached work would describe that work wrongly.

	// Labels for a single model request, set immediately before the call by
	// every call site that has an opinion. An inherited label is worse than no
	// label, because usage is aggregated by purpose and a background call
	// would be filed under whatever its parent was doing.
	LLMCallPurposeContextKey:             false,
	LLMPromptPrefixFingerprintContextKey: false,
}

// ContextKeysClonedAcrossDetach returns the keys logger.CloneContext carries
// into a detached context, sorted so the result does not depend on map order.
func ContextKeysClonedAcrossDetach() []ContextKey {
	keys := make([]ContextKey, 0, len(contextCloneAcrossDetach))
	for key, clone := range contextCloneAcrossDetach {
		if clone {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

// ContextCloneDecision reports whether the key survives a detach, and whether
// any decision has been recorded for it at all. The second return value is
// what the coverage test uses to tell a deliberate false from an omission.
func ContextCloneDecision(key ContextKey) (clone bool, declared bool) {
	clone, declared = contextCloneAcrossDetach[key]
	return clone, declared
}
