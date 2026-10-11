package types

import (
	"errors"
	"fmt"
	"time"
)

// ChatGPTError preserves the upstream failure without treating partial output
// as a completed generation.
type ChatGPTError struct {
	Kind, Code, Param, Message, RequestID, Body string
	Status                                      int
	Cause                                       error
}

func (e *ChatGPTError) Error() string {
	return fmt.Sprintf("ChatGPT %s (status=%d, code=%s, request_id=%s): %s", e.Kind, e.Status, e.Code, e.RequestID, e.Message)
}
func (e *ChatGPTError) Unwrap() error { return e.Cause }
func IsChatGPTQuotaError(err error) bool {
	var e *ChatGPTError
	return errors.As(err, &e) && (e.Code == "subscription_sharing_usage_limit_exceeded" || e.Code == "subscription_sharing_usage_unavailable")
}

// ChatGPTDeferredError delays work without consuming its ordinary retry budget.
type ChatGPTDeferredError struct {
	Reason  string
	RetryAt time.Time
	Cause   error
}

func (e *ChatGPTDeferredError) Error() string {
	message := "ChatGPT generation paused: " + e.Reason
	if e.Cause != nil {
		message += "; " + e.Cause.Error()
	}
	return message
}
func (e *ChatGPTDeferredError) Unwrap() error { return e.Cause }
func IsChatGPTDeferred(err error) bool {
	var e *ChatGPTDeferredError
	return errors.As(err, &e)
}

// IsChatGPTPermanentError identifies requests that must be corrected before
// retrying. A quota deferral takes precedence over any fallback failure it wraps.
func IsChatGPTPermanentError(err error) bool {
	if IsChatGPTDeferred(err) {
		return false
	}
	var e *ChatGPTError
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "subscription_sharing_user_not_eligible",
		"subscription_sharing_unsupported_capability",
		"subscription_sharing_route_not_supported",
		"chatpass_v2_scope_not_authorized",
		"chatpass_v2_invalid_authorization_context":
		return true
	default:
		return false
	}
}

// ChatGPTPolicy is one durable generation budget for this personal deployment.
type ChatGPTPolicy struct {
	ID              int        `json:"-" gorm:"primaryKey"`
	DailyLimit      *int       `json:"daily_limit"`
	DailyUsed       int        `json:"daily_used"`
	Day             string     `json:"day"`
	Paused          bool       `json:"paused"`
	Reason          string     `json:"reason"`
	RetryAt         *time.Time `json:"retry_at,omitempty"`
	FallbackModelID string     `json:"fallback_model_id"`
	UpdatedAt       time.Time  `json:"-"`
}

func (ChatGPTPolicy) TableName() string { return "chatgpt_policy" }
