package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/application/service"
	"reliquary/internal/chatgpt"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// These commands run only the existing OAuth manager, without starting the
// application or its workers. Callback codes are accepted only from stdin or
// an owner-only file, never from command arguments or token JSON.
func runMaintenanceChatGPT(ctx context.Context, db *gorm.DB, args []string, input io.Reader, output io.Writer, manager *chatgpt.Manager) error {
	if len(args) == 0 {
		return errors.New("ChatGPT maintenance command is required")
	}
	if utils.GetAESKey() == nil {
		return errors.New("ChatGPT sign-in requires a new 32-byte SYSTEM_AES_KEY")
	}
	if manager == nil {
		manager = chatgpt.NewManager(db)
	}
	emit := func(value any) error { return json.NewEncoder(output).Encode(value) }
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	switch args[0] {
	case "chatgpt-login":
		id := flags.String("id", "", "existing restored account ID")
		callback := flags.String("callback", "http://127.0.0.1:8080/auth/callback", "local OAuth redirect URL")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *id == "" {
			return errors.New("chatgpt-login requires --id ACCOUNT_ID and --callback LOCAL_URL")
		}
		var account struct{ Status, Credentials string }
		if err := db.WithContext(ctx).Table("chatgpt_accounts").Select("status,credentials").Where("id=?", *id).Take(&account).Error; err != nil {
			return errors.New("restored ChatGPT account not found")
		}
		// BeginLogin can include an ID-token hint for a connected account. This
		// offline recovery entrance permits only scrubbed accounts, so the
		// authorization URL cannot disclose an existing token.
		if account.Status != chatgpt.StatusNeedsLogin || account.Credentials != "" {
			return errors.New("offline sign-in requires a restored needs_login account with excluded credentials")
		}
		start, err := manager.BeginLogin(ctx, *callback, *id)
		if err != nil {
			return errors.New("cannot start ChatGPT sign-in; check the account, local callback URL and encryption key")
		}
		endpoint, err := offlineChatGPTAuthorizationURL(start.AuthorizationURL)
		if err != nil {
			return err
		}
		return emit(map[string]any{"account_id": *id, "authorization_url": endpoint, "expires_at": start.ExpiresAt})
	case "chatgpt-complete":
		path := flags.String("input", "-", "owner-only callback JSON file or - for stdin")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
			return errors.New("chatgpt-complete requires --input FILE or stdin")
		}
		if *path != "-" {
			file, err := os.Open(*path)
			if err != nil {
				return errors.New("cannot open ChatGPT callback input")
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return errors.New("ChatGPT callback input must be a regular owner-only file, such as mode 0600")
			}
			input = file
		}
		data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
		if err != nil || len(data) > 1<<20 {
			return errors.New("ChatGPT callback JSON must be readable and no larger than 1 MiB")
		}
		var request struct {
			CallbackURL string `json:"callback_url"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil {
			return errors.New("invalid ChatGPT callback JSON")
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return errors.New("ChatGPT callback input must contain exactly one JSON object")
		}
		callback, err := url.Parse(request.CallbackURL)
		if err != nil || callback.Fragment != "" {
			return errors.New("invalid ChatGPT callback URL")
		}
		query, err := url.ParseQuery(callback.RawQuery)
		if err != nil {
			return errors.New("invalid ChatGPT callback query")
		}
		for _, values := range query {
			if len(values) != 1 {
				return errors.New("duplicate ChatGPT callback query fields are rejected")
			}
		}
		state := query.Get("state")
		if state == "" || strings.HasPrefix(state, types.MaintenanceExcludedCredentialPrefix) {
			return errors.New("invalid or expired ChatGPT authorization state")
		}
		callback.RawQuery, callback.ForceQuery = "", false
		if chatgpt.ValidateCallbackURL(callback.String()) != nil {
			return errors.New("ChatGPT callback must use the configured local redirect URL")
		}
		var attempt struct {
			CallbackURL string
			AccountID   *string
		}
		if err := db.WithContext(ctx).Table("chatgpt_login_attempts").Select("callback_url,account_id").Where("state=?", state).Take(&attempt).Error; err != nil || attempt.AccountID == nil || callback.String() != attempt.CallbackURL {
			return errors.New("ChatGPT callback does not match the restored account authorization attempt")
		}
		if query.Get("error") != "" {
			if err := manager.CancelLogin(ctx, state, query.Get("error")); err != nil {
				return errors.New("invalid or expired ChatGPT authorization state")
			}
			return errors.New("ChatGPT authorization was declined; start a new sign-in")
		}
		if query.Get("code") == "" {
			return errors.New("ChatGPT callback is missing its authorization code")
		}
		account, err := manager.CompleteLogin(ctx, state, query.Get("code"), query.Get("client_id"))
		if err != nil {
			var retry *chatgpt.AuthorizationRetryError
			if errors.As(err, &retry) {
				endpoint, endpointErr := manager.LoginAuthorizationURL(ctx, retry.State)
				if endpointErr == nil {
					endpoint, endpointErr = offlineChatGPTAuthorizationURL(endpoint)
				}
				if endpointErr == nil {
					if emitErr := emit(map[string]any{"account_id": *attempt.AccountID, "authorization_url": endpoint, "expires_at": retry.ExpiresAt, "status": "authorization_retry_required"}); emitErr != nil {
						return emitErr
					}
				}
				return errors.New("ChatGPT authorization needs a fresh attempt; open the returned authorization_url")
			}
			return errors.New("ChatGPT sign-in failed validation or the provider is unavailable; start a new sign-in")
		}
		restored := []string{}
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var models []types.Model
			if err := tx.Where("source=? AND deleted_at IS NULL AND parameters->'extra_config'->>'chatgpt_account_id'=?", types.ModelSourceChatGPT, account.ID).Order("id").Find(&models).Error; err != nil {
				return err
			}
			for index := range models {
				model := &models[index]
				changed, err := service.CompleteMaintenanceModelState(ctx, tx, model)
				if err != nil {
					return err
				}
				if changed {
					if err := tx.Model(&types.Model{}).Where("id=?", model.ID).Updates(map[string]any{"parameters": model.Parameters, "status": model.Status, "updated_at": time.Now().UTC()}).Error; err != nil {
						return err
					}
					restored = append(restored, model.ID)
				}
			}
			return nil
		}); err != nil {
			return errors.New("ChatGPT is connected, but linked model completion failed; keep the application offline and check configuration")
		}
		return emit(map[string]any{"account_id": account.ID, "status": account.Status, "restored_model_ids": restored})
	default:
		return errors.New("unsupported ChatGPT maintenance command")
	}
}

// A concurrent login may have connected the account after our initial check.
// OAuth permits a fresh sign-in without an ID-token hint; always omit it from
// CLI output rather than relying on the earlier credentials-empty check.
func offlineChatGPTAuthorizationURL(raw string) (string, error) {
	endpoint, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("cannot construct ChatGPT authorization URL")
	}
	query := endpoint.Query()
	query.Del("id_token_hint")
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}
