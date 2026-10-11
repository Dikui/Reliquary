package queue

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestDeadLetterPreservesDiagnosticsAndPayload(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"tenant_id": 42, "knowledge_base_id": "kb-abc", "entry_id": "k-1"})
	row := BuildDeadLetter(NewTask("knowledge:process", payload), errors.New("boom"), 3)
	require.Equal(t, "knowledge:process", row.TaskType)
	require.Equal(t, types.SingleSpaceID, row.SpaceID)
	require.Equal(t, payload, []byte(row.Payload))
	require.Equal(t, types.TaskScopeKnowledgeBase, row.Scope)
	require.Equal(t, "kb-abc", row.ScopeID)
	require.Equal(t, "k-1", row.RelatedID)
	require.Equal(t, "boom", row.LastError)
	require.Equal(t, 3, row.FailCount)
	require.False(t, row.FailedAt.IsZero())
	require.Nil(t, BuildDeadLetter(nil, errors.New("err"), 0))
}

func TestDeadLetterUnknownPayloadAndEmptyPayload(t *testing.T) {
	payload := []byte(`{"some_unrelated":"value"}`)
	row := BuildDeadLetter(NewTask("future:weird", payload), errors.New("nope"), 1)
	require.Equal(t, types.TaskScopeUnknown, row.Scope)
	require.Empty(t, row.ScopeID)
	require.Equal(t, payload, []byte(row.Payload))
	row = BuildDeadLetter(NewTask("any:task", nil), errors.New("err"), 0)
	require.Equal(t, "{}", string(row.Payload))
	require.Zero(t, row.FailCount)
	row = BuildDeadLetter(NewTask("any:task", []byte("malformed")), errors.New("err"), 1)
	require.Equal(t, types.TaskScopeUnknown, row.Scope)
	require.Equal(t, "malformed", string(row.Payload))
}

func TestDeadLetterLongErrorTruncated(t *testing.T) {
	row := BuildDeadLetter(NewTask("any", []byte("{}")), errors.New(strings.Repeat("x", 16384)), 1)
	require.Len(t, row.LastError, 8192)
	require.True(t, strings.HasSuffix(row.LastError, "...(truncated)"))
}

func TestDeadLetterScopePriority(t *testing.T) {
	tests := []struct{ name, payload, scope, id string }{
		{"knowledge_base_id wins over entry_id", `{"knowledge_base_id":"kb1","entry_id":"k1"}`, types.TaskScopeKnowledgeBase, "kb1"},
		{"entry_id only", `{"entry_id":"k-only"}`, types.TaskScopeEntry, "k-only"},
		{"space_id fallback", `{"tenant_id":7}`, types.TaskScopeSpace, "7"},
		{"no identifiers", `{}`, types.TaskScopeUnknown, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := BuildDeadLetter(NewTask("fixture", []byte(tc.payload)), errors.New("err"), 1)
			require.Equal(t, tc.scope, row.Scope)
			require.Equal(t, tc.id, row.ScopeID)
		})
	}
}
