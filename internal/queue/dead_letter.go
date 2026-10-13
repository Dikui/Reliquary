package queue

import (
	"context"
	"reliquary/internal/types"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// BuildDeadLetter keeps the existing archive scope and diagnostic fields.
func BuildDeadLetter(t *Task, taskErr error, attempts int) *types.TaskDeadLetter {
	if t == nil {
		return nil
	}
	var p types.DocumentProcessPayload
	_ = types.UnmarshalSpacePayload(t.Payload(), &p)
	scope, id := types.TaskScopeUnknown, ""
	if p.LibraryID != "" {
		scope, id = types.TaskScopeLibrary, p.LibraryID
	} else if p.EntryID != "" {
		scope, id = types.TaskScopeEntry, p.EntryID
	} else if p.SpaceID != 0 {
		scope, id = types.TaskScopeSpace, strconv.FormatUint(p.SpaceID, 10)
	}
	payload := t.Payload()
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	message := NormalizeError(taskErr.Error())
	return &types.TaskDeadLetter{SpaceID: types.SingleSpaceID, TaskType: t.Type(), Scope: scope, ScopeID: id, RelatedID: p.EntryID, Payload: payload, LastError: message, FailCount: attempts, FailedAt: time.Now().UTC()}
}

// NormalizeError keeps diagnostic text within the archive's 8KB limit and
// valid for PostgreSQL UTF-8 text columns, including errors from providers.
func NormalizeError(message string) string {
	message = strings.ReplaceAll(strings.ToValidUTF8(message, "\uFFFD"), "\x00", "")
	const limit, suffix = 8192, "...(truncated)"
	if len(message) <= limit {
		return message
	}
	end := limit - len(suffix)
	for !utf8.ValidString(message[:end]) {
		end--
	}
	return message[:end] + suffix
}
func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
