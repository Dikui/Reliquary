package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (m *Manager) replaceTokens(account *accountRecord, tokens *tokenResponse, old credentials) error {
	if tokens.IDToken == "" {
		tokens.IDToken = old.IDToken
	}
	if tokens.Scope == "" {
		tokens.Scope = account.Scopes
	}
	if !checkScopes(tokens.Scope) {
		return ErrInsufficientScopes
	}
	if err := validTokenSet(tokens); err != nil {
		return err
	}
	encrypted, err := encryptJSON(credentials{AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, IDToken: tokens.IDToken})
	if err != nil {
		return err
	}
	earliest, err := parseEarliestRefresh(tokens.EarliestRefreshAt)
	if err != nil {
		return err
	}
	expires := m.now().Add(time.Duration(tokens.ExpiresIn) * time.Second).UTC()
	account.Credentials, account.Scopes = encrypted, tokens.Scope
	account.ExpiresAt, account.EarliestRefreshAt = &expires, earliest
	account.Status, account.LastError = StatusConnected, ""
	account.UpdatedAt = m.now().UTC()
	return nil
}

func terminalRefreshError(err error) bool {
	var endpoint *EndpointError
	if !errors.As(err, &endpoint) {
		return errors.Is(err, ErrInsufficientScopes) || errors.Is(err, ErrInvalidIdentity)
	}
	switch endpoint.Code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	default:
		return false
	}
}

func refreshErrorCode(err error) string {
	var endpoint *EndpointError
	if errors.As(err, &endpoint) {
		return endpoint.Code
	}
	if errors.Is(err, ErrInsufficientScopes) {
		return "insufficient_scopes"
	}
	if errors.Is(err, ErrEncryptionRequired) {
		return "encryption_required"
	}
	if errors.Is(err, ErrInvalidIdentity) {
		return "invalid_identity"
	}
	return "refresh_unavailable"
}

// AccessToken refreshes near expiry. A PostgreSQL row lock also serializes
// refreshes between Manager instances and server processes.
func (m *Manager) AccessToken(ctx context.Context, accountID string) (string, error) {
	if accountID == "" {
		return "", ErrNeedsLogin
	}
	var token string
	var refreshErr error
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account accountRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", accountID).Error; err != nil {
			return err
		}
		if account.Credentials == "" || account.Status == StatusSignedOut {
			refreshErr = ErrNeedsLogin
			return nil
		}
		var saved credentials
		if err := decryptJSON(account.Credentials, &saved); err != nil {
			refreshErr = err
			account.Status, account.LastError = StatusNeedsLogin, refreshErrorCode(err)
			return tx.Save(&account).Error
		}
		now := m.now()
		refreshDue := account.ExpiresAt == nil || !account.ExpiresAt.After(now.Add(2*time.Minute)) || account.Status == StatusNeedsLogin
		if refreshDue && account.EarliestRefreshAt != nil && now.Before(*account.EarliestRefreshAt) {
			if account.ExpiresAt != nil && now.Before(*account.ExpiresAt) && account.Status == StatusConnected {
				token = saved.AccessToken
				return nil
			}
			refreshErr = fmt.Errorf("chatgpt: token refresh is not yet available")
			account.Status, account.LastError = StatusNeedsLogin, "refresh_not_yet_available"
			return tx.Save(&account).Error
		}
		if !refreshDue {
			token = saved.AccessToken
			return nil
		}
		if saved.RefreshToken == "" {
			refreshErr = ErrNeedsLogin
			account.Status, account.LastError = StatusNeedsLogin, "missing_refresh_token"
			return tx.Save(&account).Error
		}
		tokens, err := m.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {account.ClientID}, "refresh_token": {saved.RefreshToken}, "resource": {Resource}})
		if err == nil && tokens.IDToken != "" {
			var identity *identityClaims
			identity, err = m.validateIdentity(ctx, tokens.IDToken, account.ClientID, "")
			if err == nil && (identity.Subject != account.Subject || identity.Issuer != account.Issuer) {
				err = ErrInvalidIdentity
			}
		}
		if err == nil {
			err = m.replaceTokens(&account, tokens, saved)
		}
		if err != nil {
			refreshErr = err
			account.Status, account.LastError = StatusNeedsLogin, refreshErrorCode(err)
			if terminalRefreshError(err) {
				account.Credentials = ""
			}
		} else {
			token = tokens.AccessToken
		}
		return tx.Save(&account).Error
	})
	if err != nil {
		return "", err
	}
	if refreshErr != nil {
		return "", refreshErr
	}
	return token, nil
}

func (m *Manager) revoke(ctx context.Context, clientID, refreshToken string) bool {
	doc, err := m.discovery(ctx)
	if err != nil || doc.RevocationEndpoint == "" {
		return false
	}
	form := url.Values{"token": {refreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {clientID}}
	for attempt := 0; attempt < 3; attempt++ {
		_, status, _, err := m.request(ctx, http.MethodPost, doc.RevocationEndpoint, form, "")
		if err == nil && status == http.StatusOK {
			return true
		}
		if err == nil && status < 500 {
			return false
		}
		if attempt == 2 {
			break
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return false
}

func (m *Manager) Logout(ctx context.Context, accountID string) (*LogoutResult, error) {
	result := &LogoutResult{RemoteRevocationConfirmed: true}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account accountRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", accountID).Error; err != nil {
			return err
		}
		if account.Credentials != "" {
			var saved credentials
			if decryptJSON(account.Credentials, &saved) != nil || saved.RefreshToken == "" {
				result.RemoteRevocationConfirmed = false
			} else {
				result.RemoteRevocationConfirmed = m.revoke(ctx, account.ClientID, saved.RefreshToken)
			}
		}
		account.Credentials, account.Status, account.LastError = "", StatusSignedOut, ""
		if !result.RemoteRevocationConfirmed {
			account.LastError = "revocation_unconfirmed"
		}
		account.ExpiresAt, account.EarliestRefreshAt = nil, nil
		account.UpdatedAt = m.now().UTC()
		if err := tx.Where("account_id = ?", accountID).Delete(&loginAttempt{}).Error; err != nil {
			return err
		}
		return tx.Save(&account).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
