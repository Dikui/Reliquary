package types

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatGPTPermanentErrorCodes(t *testing.T) {
	for _, code := range []string{
		"subscription_sharing_user_not_eligible",
		"subscription_sharing_unsupported_capability",
		"subscription_sharing_route_not_supported",
		"chatpass_v2_scope_not_authorized",
		"chatpass_v2_invalid_authorization_context",
	} {
		t.Run(code, func(t *testing.T) {
			upstream := &ChatGPTError{Code: code, Status: 403, RequestID: "req_permanent"}
			err := fmt.Errorf("generation: %w", upstream)
			require.True(t, IsChatGPTPermanentError(err))
			require.False(t, IsChatGPTPermanentError(&ChatGPTDeferredError{Reason: "quota", Cause: errors.Join(err, errors.New("fallback failed"))}))
			require.False(t, IsChatGPTPermanentError(errors.Join(&ChatGPTDeferredError{Reason: "quota"}, err)))
		})
	}
	for _, code := range []string{
		"subscription_sharing_usage_limit_exceeded",
		"subscription_sharing_usage_unavailable",
		"subscription_sharing_invalid_user",
		"subscription_sharing_user_unavailable",
		"server_error", "", "unknown_error",
	} {
		t.Run(code, func(t *testing.T) {
			require.False(t, IsChatGPTPermanentError(&ChatGPTError{Code: code, Status: 403}))
		})
	}
	require.False(t, IsChatGPTPermanentError(nil))
	require.False(t, IsChatGPTPermanentError(errors.New("connection interrupted")))
}
