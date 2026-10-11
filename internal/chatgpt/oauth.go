package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/utils"
)

func ValidateCallbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "/auth/callback" || u.RawQuery != "" || u.Fragment != "" {
		return ErrInvalidCallback
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return ErrInvalidCallback
	}
	return nil
}

func randomValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HostID returns the persisted opaque ID for this database-backed installation.
func (m *Manager) HostID(ctx context.Context) (string, error) {
	host := hostRecord{ID: 1, ExtAgentHostID: "urn:uuid:" + uuid.NewString(), CreatedAt: m.now().UTC()}
	if err := m.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&host).Error; err != nil {
		return "", err
	}
	if err := m.db.WithContext(ctx).First(&host, "id = 1").Error; err != nil {
		return "", err
	}
	return host.ExtAgentHostID, nil
}

func encryptJSON(v any) (string, error) {
	key := utils.GetAESKey()
	if key == nil {
		return "", ErrEncryptionRequired
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return utils.EncryptAESGCM(string(b), key)
}

func decryptJSON(raw string, v any) error {
	if utils.GetAESKey() == nil {
		return ErrEncryptionRequired
	}
	if !strings.HasPrefix(raw, utils.EncPrefix) {
		return fmt.Errorf("chatgpt: unencrypted credential storage rejected")
	}
	plain, err := utils.DecryptStoredSecret(raw)
	if err != nil {
		return fmt.Errorf("chatgpt: credential decryption failed")
	}
	if err := json.Unmarshal([]byte(plain), v); err != nil {
		return fmt.Errorf("chatgpt: invalid credential storage")
	}
	return nil
}

func (m *Manager) BeginLogin(ctx context.Context, callbackURL, accountID string) (*LoginStart, error) {
	if utils.GetAESKey() == nil {
		return nil, ErrEncryptionRequired
	}
	if err := ValidateCallbackURL(callbackURL); err != nil {
		return nil, err
	}
	hostID, err := m.HostID(ctx)
	if err != nil {
		return nil, err
	}
	state, err := randomValue()
	if err != nil {
		return nil, err
	}
	nonce, err := randomValue()
	if err != nil {
		return nil, err
	}
	verifier, err := randomValue()
	if err != nil {
		return nil, err
	}
	verifierCipher, err := encryptJSON(verifier)
	if err != nil {
		return nil, err
	}
	attempt := loginAttempt{State: state, Nonce: nonce, Verifier: verifierCipher, CallbackURL: callbackURL, ClientID: DynamicClientID, ExpiresAt: m.now().Add(10 * time.Minute).UTC()}
	query := url.Values{
		"response_type": {"code"}, "redirect_uri": {callbackURL}, "scope": {RequestedScopes}, "resource": {Resource},
		"state": {state}, "nonce": {nonce}, "ext_agent_host_id": {hostID}, "code_challenge_method": {"S256"},
	}
	if accountID == "" {
		query.Set("agent_name_hint", "Reliquary")
	} else {
		var account accountRecord
		if err := m.db.WithContext(ctx).First(&account, "id = ?", accountID).Error; err != nil {
			return nil, err
		}
		attempt.AccountID = &account.ID
		attempt.ClientID = account.ClientID
		if account.Email != "" {
			query.Set("login_hint", account.Email)
		}
		if account.Credentials != "" {
			var saved credentials
			if err := decryptJSON(account.Credentials, &saved); err != nil {
				return nil, err
			}
			if saved.IDToken != "" {
				query.Set("id_token_hint", saved.IDToken)
			}
		}
	}
	query.Set("client_id", attempt.ClientID)
	hash := sha256.Sum256([]byte(verifier))
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(hash[:]))
	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", m.now()).Delete(&loginAttempt{}).Error; err != nil {
			return err
		}
		return tx.Create(&attempt).Error
	}); err != nil {
		return nil, err
	}
	return &LoginStart{AuthorizationURL: m.endpoints.Authorization + "?" + query.Encode(), ExpiresAt: attempt.ExpiresAt, State: state}, nil
}

// LoginAuthorizationURL is used by the browser redirect endpoint. ID-token
// hints therefore go directly to OpenAI and never through a frontend JSON body.
func (m *Manager) LoginAuthorizationURL(ctx context.Context, state string) (string, error) {
	var attempt loginAttempt
	if state == "" || m.db.WithContext(ctx).First(&attempt, "state = ?", state).Error != nil || !attempt.ExpiresAt.After(m.now()) {
		return "", ErrInvalidState
	}
	var verifier string
	if err := decryptJSON(attempt.Verifier, &verifier); err != nil {
		return "", err
	}
	hostID, err := m.HostID(ctx)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {attempt.ClientID}, "response_type": {"code"}, "redirect_uri": {attempt.CallbackURL}, "scope": {RequestedScopes}, "resource": {Resource}, "state": {attempt.State}, "nonce": {attempt.Nonce}, "ext_agent_host_id": {hostID}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
	if attempt.ClientID == DynamicClientID {
		query.Set("agent_name_hint", "Reliquary")
	}
	if attempt.AccountID != nil {
		var account accountRecord
		if err := m.db.WithContext(ctx).First(&account, "id = ?", *attempt.AccountID).Error; err != nil {
			return "", err
		}
		if account.Email != "" {
			query.Set("login_hint", account.Email)
		}
		if account.Credentials != "" {
			var saved credentials
			if err := decryptJSON(account.Credentials, &saved); err != nil {
				return "", err
			}
			if saved.IDToken != "" {
				query.Set("id_token_hint", saved.IDToken)
			}
		}
	}
	return m.endpoints.Authorization + "?" + query.Encode(), nil
}

func (m *Manager) consumeAttempt(ctx context.Context, state string) (*loginAttempt, error) {
	if state == "" {
		return nil, ErrInvalidState
	}
	var attempt loginAttempt
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&attempt, "state = ?", state).Error; err != nil {
			return ErrInvalidState
		}
		if subtle.ConstantTimeCompare([]byte(attempt.State), []byte(state)) != 1 || !attempt.ExpiresAt.After(m.now()) {
			return ErrInvalidState
		}
		return tx.Delete(&attempt).Error
	})
	return &attempt, err
}

func (m *Manager) retryAuthorization(ctx context.Context, previous *loginAttempt, clientID string, cause error) error {
	state, err := randomValue()
	if err != nil {
		return err
	}
	nonce, err := randomValue()
	if err != nil {
		return err
	}
	verifier, err := randomValue()
	if err != nil {
		return err
	}
	verifierCipher, err := encryptJSON(verifier)
	if err != nil {
		return err
	}
	// The old code/state have already been consumed. No unvalidated account or
	// credential record is saved, only this short-lived continuation attempt.
	attempt := loginAttempt{State: state, Nonce: nonce, Verifier: verifierCipher, CallbackURL: previous.CallbackURL, ClientID: clientID, AccountID: previous.AccountID, ExpiresAt: m.now().Add(10 * time.Minute).UTC()}
	if err := m.db.WithContext(ctx).Create(&attempt).Error; err != nil {
		return err
	}
	return &AuthorizationRetryError{State: state, CallbackURL: attempt.CallbackURL, ExpiresAt: attempt.ExpiresAt, cause: cause}
}

// CancelLogin validates and consumes state even when the user declines consent.
func (m *Manager) CancelLogin(ctx context.Context, state, errorCode string) error {
	_, err := m.consumeAttempt(ctx, state)
	return err
}

func checkScopes(raw string) bool {
	granted := make(map[string]bool)
	for _, scope := range strings.Fields(raw) {
		granted[scope] = true
	}
	for _, scope := range strings.Fields(RequestedScopes) {
		if !granted[scope] {
			return false
		}
	}
	return true
}

func (m *Manager) CompleteLogin(ctx context.Context, state, code, clientID string) (*Account, error) {
	if utils.GetAESKey() == nil {
		return nil, ErrEncryptionRequired
	}
	attempt, err := m.consumeAttempt(ctx, state)
	if err != nil {
		return nil, err
	}
	if code == "" {
		return nil, fmt.Errorf("chatgpt: authorization code missing")
	}
	if attempt.ClientID == DynamicClientID {
		if clientID == "" || clientID == DynamicClientID {
			return nil, ErrInvalidClient
		}
	} else {
		if clientID != "" && clientID != attempt.ClientID {
			return nil, ErrInvalidClient
		}
		clientID = attempt.ClientID
	}
	var verifier string
	if err := decryptJSON(attempt.Verifier, &verifier); err != nil {
		return nil, err
	}
	tokens, err := m.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {attempt.CallbackURL}, "resource": {Resource}})
	if err != nil {
		var upstream *EndpointError
		if errors.As(err, &upstream) && upstream.Code == "invalid_grant" {
			return nil, m.retryAuthorization(ctx, attempt, clientID, err)
		}
		return nil, err
	}
	identity, err := m.validateIdentity(ctx, tokens.IDToken, clientID, attempt.Nonce)
	if err != nil {
		return nil, ErrInvalidIdentity
	}
	if !checkScopes(tokens.Scope) {
		return nil, ErrInsufficientScopes
	}
	if err := validTokenSet(tokens); err != nil {
		return nil, err
	}
	var result accountRecord
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if attempt.AccountID != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, "id = ?", *attempt.AccountID).Error; err != nil {
				return err
			}
			if result.Subject != identity.Subject || result.Issuer != identity.Issuer || result.ClientID != clientID {
				return ErrInvalidIdentity
			}
		} else {
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&result, "client_id = ?", clientID).Error
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			if err == gorm.ErrRecordNotFound {
				result = accountRecord{ID: uuid.NewString(), ClientID: clientID, Subject: identity.Subject, Issuer: identity.Issuer, CreatedAt: m.now().UTC()}
			}
			if result.Subject != identity.Subject || result.Issuer != identity.Issuer {
				return ErrInvalidIdentity
			}
		}
		result.Email = identity.Email
		if err := m.replaceTokens(&result, tokens, credentials{}); err != nil {
			return err
		}
		return tx.Save(&result).Error
	})
	if err != nil {
		return nil, err
	}
	account := publicAccount(result)
	return &account, nil
}

func publicAccount(record accountRecord) Account {
	return Account{ID: record.ID, ClientID: record.ClientID, Subject: record.Subject, Email: record.Email, Status: record.Status, LastError: record.LastError, Scopes: strings.Fields(record.Scopes), ExpiresAt: record.ExpiresAt, CreatedAt: record.CreatedAt, WelcomeAcknowledged: record.WelcomeAcknowledged}
}

func (m *Manager) AcknowledgeWelcome(ctx context.Context, accountID string) error {
	result := m.db.WithContext(ctx).Model(&accountRecord{}).Where("id = ? AND status = ?", accountID, StatusConnected).Update("welcome_acknowledged", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNeedsLogin
	}
	return nil
}

func (m *Manager) ListAccounts(ctx context.Context) ([]Account, error) {
	var records []accountRecord
	if err := m.db.WithContext(ctx).Order("created_at, id").Find(&records).Error; err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(records))
	for _, record := range records {
		accounts = append(accounts, publicAccount(record))
	}
	return accounts, nil
}

func (m *Manager) Status(ctx context.Context, accountID string) (*Account, error) {
	var record accountRecord
	if err := m.db.WithContext(ctx).First(&record, "id = ?", accountID).Error; err != nil {
		return nil, err
	}
	account := publicAccount(record)
	return &account, nil
}
