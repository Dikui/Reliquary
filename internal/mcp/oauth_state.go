package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"reliquary/internal/types"
)

// oauthStateTTL bounds how long an in-flight authorization may take from
// "authorize-url issued" to "callback received".
const oauthStateTTL = 10 * time.Minute

// OAuthState is the transient data needed to complete an authorization-code
// exchange. It is keyed by the opaque OAuth `state` parameter and MUST hold
// the PKCE code_verifier, which is a secret that must never reach the
// authorization server — hence server-side storage rather than encoding it
// into the state parameter.
type OAuthState struct {
	TenantID     uint64          `json:"tenant_id"`
	UserID       string          `json:"user_id"`
	Principal    types.Principal `json:"principal"`
	ServiceID    string          `json:"service_id"`
	CodeVerifier string          `json:"code_verifier"`
	ClientID     string          `json:"client_id"`
	RedirectURI  string          `json:"redirect_uri"`
	// FrontendRedirect is where the backend callback redirects the browser
	// after completing (or failing) the exchange.
	FrontendRedirect string `json:"frontend_redirect"`
}

// oauthStateStore keeps expiring, single-use authorization states in this process.
type oauthStateStore struct {
	mu       sync.Mutex
	mem      map[string]memStateEntry
	attempts map[string]memAttemptEntry
}

type memStateEntry struct {
	value     OAuthState
	expiresAt time.Time
}

// OAuthAttempt is the non-secret, authenticated status of one authorization
// flow. It is kept separately from OAuthState because Take consumes the PKCE
// state before the token exchange finishes, while the opener still needs to
// distinguish this callback from an older token stored for the same service.
type OAuthAttempt struct {
	TenantID  uint64          `json:"tenant_id"`
	Principal types.Principal `json:"principal"`
	ServiceID string          `json:"service_id"`
	Completed bool            `json:"completed"`
}

type memAttemptEntry struct {
	value     OAuthAttempt
	expiresAt time.Time
}

func newOAuthStateStore() *oauthStateStore {
	return &oauthStateStore{mem: make(map[string]memStateEntry), attempts: make(map[string]memAttemptEntry)}
}

// Put stores a state with a fixed TTL.
func (s *oauthStateStore) Put(ctx context.Context, state string, value OAuthState) error {
	attempt := OAuthAttempt{
		TenantID:  value.TenantID,
		Principal: value.Principal.Normalize(),
		ServiceID: value.ServiceID,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcExpiredLocked(time.Now())
	expiresAt := time.Now().Add(oauthStateTTL)
	s.mem[state] = memStateEntry{value: value, expiresAt: expiresAt}
	s.attempts[state] = memAttemptEntry{value: attempt, expiresAt: expiresAt}
	return nil
}

// CompleteAttempt marks an authorization attempt complete only after the code
// exchange has successfully persisted a token.
func (s *oauthStateStore) CompleteAttempt(ctx context.Context, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcExpiredLocked(time.Now())
	entry, ok := s.attempts[state]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.attempts, state)
		return fmt.Errorf("oauth attempt not found or expired")
	}
	entry.value.Completed = true
	entry.expiresAt = time.Now().Add(oauthStateTTL)
	s.attempts[state] = entry
	return nil
}

// Attempt returns the status record for one authorization flow.
func (s *oauthStateStore) Attempt(ctx context.Context, state string) (OAuthAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcExpiredLocked(time.Now())
	entry, ok := s.attempts[state]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.attempts, state)
		return OAuthAttempt{}, fmt.Errorf("oauth attempt not found or expired")
	}
	return entry.value, nil
}

// Take retrieves and deletes a state (single-use). Returns an error if the
// state is unknown or expired.
func (s *oauthStateStore) Take(ctx context.Context, state string) (OAuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcExpiredLocked(time.Now())
	entry, ok := s.mem[state]
	if !ok {
		return OAuthState{}, fmt.Errorf("oauth state not found or expired")
	}
	delete(s.mem, state)
	if time.Now().After(entry.expiresAt) {
		return OAuthState{}, fmt.Errorf("oauth state not found or expired")
	}
	return entry.value, nil
}

// gcExpiredLocked removes expired entries during store access.
func (s *oauthStateStore) gcExpiredLocked(now time.Time) {
	for key, entry := range s.mem {
		if !now.Before(entry.expiresAt) {
			delete(s.mem, key)
		}
	}
	for key, entry := range s.attempts {
		if !now.Before(entry.expiresAt) {
			delete(s.attempts, key)
		}
	}
}
