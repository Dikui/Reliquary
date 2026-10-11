// Package chatgpt owns Reliquary's independent ChatGPT account registrations.
package chatgpt

import (
	"context"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/utils"
)

const (
	StatusConnected  = "connected"
	StatusNeedsLogin = "needs_login"
	StatusSignedOut  = "signed_out"
	DynamicClientID  = "dynamic_agent_client"
	Resource         = "https://api.openai.com/v1"
	RequestedScopes  = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

var (
	ErrNeedsLogin         = errors.New("chatgpt: sign in again")
	ErrEncryptionRequired = errors.New("chatgpt: SYSTEM_AES_KEY must contain exactly 32 bytes")
	ErrInvalidState       = errors.New("chatgpt: invalid or expired authorization state")
	ErrInvalidCallback    = errors.New("chatgpt: callback must be http://127.0.0.1:<port>/auth/callback")
	ErrInsufficientScopes = errors.New("chatgpt: required ChatGPT plan scopes were not granted")
	ErrInvalidIdentity    = errors.New("chatgpt: ID token validation failed")
	ErrInvalidClient      = errors.New("chatgpt: issued client ID does not match the authorization attempt")
)

// Account is a safe view of one registered account. Credentials never appear here.
type Account struct {
	ID                  string     `json:"id"`
	ClientID            string     `json:"client_id"`
	Subject             string     `json:"subject"`
	Email               string     `json:"email"`
	Status              string     `json:"status"`
	LastError           string     `json:"last_error,omitempty"`
	Scopes              []string   `json:"scopes"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	WelcomeAcknowledged bool       `json:"welcome_acknowledged"`
}

type LoginStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
	State            string    `json:"-"`
}

// AuthorizationRetryError offers an explicit fresh authorization after an
// unusable code. The pending registration retains its issued client ID.
type AuthorizationRetryError struct {
	State       string
	CallbackURL string
	ExpiresAt   time.Time
	cause       error
}

func (e *AuthorizationRetryError) Error() string {
	return "chatgpt: authorization code is unusable; continue with a fresh authorization"
}
func (e *AuthorizationRetryError) Unwrap() error { return e.cause }

type LogoutResult struct {
	RemoteRevocationConfirmed bool `json:"remote_revocation_confirmed"`
}

type Model struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Visibility  string `json:"visibility"`
}

// Endpoints is injected by local tests. The application uses DefaultEndpoints.
type Endpoints struct {
	Issuer        string
	Authorization string
	Token         string
	Discovery     string
	Models        string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		Issuer:        "https://auth.openai.com",
		Authorization: "https://auth.openai.com/api/accounts/authorize",
		Token:         "https://auth.openai.com/api/accounts/oauth/token",
		Discovery:     "https://auth.openai.com/.well-known/openid-configuration",
		Models:        "https://api.openai.com/v1/models",
	}
}

type Option func(*Manager)

func WithHTTPClient(client *http.Client) Option { return func(m *Manager) { m.client = client } }
func WithEndpoints(endpoints Endpoints) Option  { return func(m *Manager) { m.endpoints = endpoints } }
func WithClock(now func() time.Time) Option     { return func(m *Manager) { m.now = now } }

type Manager struct {
	db        *gorm.DB
	client    *http.Client
	endpoints Endpoints
	now       func() time.Time
}

func NewManager(db *gorm.DB, opts ...Option) *Manager {
	config := utils.DefaultSSRFSafeHTTPClientConfig()
	config.Timeout = 20 * time.Second
	m := &Manager{db: db, client: utils.NewSSRFSafeHTTPClient(config), endpoints: DefaultEndpoints(), now: time.Now}
	for _, opt := range opts {
		opt(m)
	}
	// OAuth codes and refresh tokens must never follow an HTTP redirect.
	client := *m.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	m.client = &client
	return m
}

type accountRecord struct {
	ID                  string `gorm:"type:uuid;primaryKey"`
	ClientID            string
	Issuer              string
	Subject             string
	Email               string
	Credentials         string
	Scopes              string
	ExpiresAt           *time.Time
	EarliestRefreshAt   *time.Time
	Status              string
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	WelcomeAcknowledged bool
}

func (accountRecord) TableName() string { return "chatgpt_accounts" }

type loginAttempt struct {
	State       string `gorm:"primaryKey"`
	Nonce       string
	Verifier    string
	CallbackURL string
	ClientID    string
	AccountID   *string `gorm:"type:uuid"`
	ExpiresAt   time.Time
}

func (loginAttempt) TableName() string { return "chatgpt_login_attempts" }

type hostRecord struct {
	ID             int `gorm:"primaryKey"`
	ExtAgentHostID string
	CreatedAt      time.Time
}

func (hostRecord) TableName() string { return "chatgpt_hosts" }

type credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

// Ensure the access-token provider can be consumed without depending on Manager.
type TokenProvider interface {
	AccessToken(context.Context, string) (string, error)
}
