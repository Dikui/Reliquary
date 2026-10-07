package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type captureKBActivityAudit struct {
	entry *types.AuditLog
}

func (c *captureKBActivityAudit) Log(_ context.Context, entry *types.AuditLog) error {
	c.entry = entry
	return nil
}

func (*captureKBActivityAudit) LogDenied(context.Context, *gin.Context, uint64, string, string, types.TenantRole) error {
	return nil
}

func (*captureKBActivityAudit) List(context.Context, uint64, *interfaces.AuditLogQuery) ([]*types.AuditLog, error) {
	return nil, nil
}

func (*captureKBActivityAudit) Purge(context.Context, int) (int64, error) { return 0, nil }

func TestRecordKBActivityCarriesInitiatorAndTaskMetadata(t *testing.T) {
	ctx := types.TaskInitiator{UserID: "user-1", Role: types.TenantRoleAdmin}.Apply(context.Background())
	ctx = withKBActivityTask(ctx, "task-1", "user")
	audit := &captureKBActivityAudit{}

	recordKBActivity(ctx, audit, 7, "kb-1", types.AuditActionKnowledgeMoveCompleted,
		"knowledge_move", "task-1", types.AuditOutcomeSuccess, map[string]any{"count": 2})

	if audit.entry == nil {
		t.Fatal("expected an activity entry")
	}
	if audit.entry.ActorUserID != "user-1" || audit.entry.ActorRole != "admin" {
		t.Fatalf("actor = %q/%q", audit.entry.ActorUserID, audit.entry.ActorRole)
	}
	var details map[string]any
	if err := json.Unmarshal(audit.entry.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["task_id"] != "task-1" || details["trigger"] != "user" || details["count"] != float64(2) {
		t.Fatalf("details = %#v", details)
	}
}

func TestKBActivityAppendSampleTitles(t *testing.T) {
	details := map[string]any{"count": 3}
	kbActivityAppendSampleTitles(details, "  Alpha  ", "Beta", "Alpha", "Gamma")
	if details["title"] != "Alpha" {
		t.Fatalf("title = %#v", details["title"])
	}
	titles, ok := details["titles"].([]string)
	if !ok || len(titles) != 3 {
		t.Fatalf("titles = %#v", details["titles"])
	}

	single := map[string]any{"count": 1}
	kbActivityAppendSampleTitles(single, "Only one")
	if single["title"] != "Only one" {
		t.Fatalf("single title = %#v", single["title"])
	}
	if _, exists := single["titles"]; exists {
		t.Fatalf("single titles should be omitted: %#v", single)
	}
}

func TestRecordKBActivityCanSuppressCompositeTaskChildren(t *testing.T) {
	audit := &captureKBActivityAudit{}
	ctx := withKBActivitySuppressed(context.Background())
	recordKBActivity(ctx, audit, 7, "kb-1", types.AuditActionKnowledgeCreated,
		"knowledge", "knowledge-1", types.AuditOutcomeAccepted, nil)
	if audit.entry != nil {
		t.Fatalf("suppressed child activity was recorded: %#v", audit.entry)
	}
}

func TestRecordWikiContentActivityUsesUnifiedActivityFeed(t *testing.T) {
	audit := &captureKBActivityAudit{}
	RecordWikiContentActivity(context.Background(), audit, 7, "kb-1", map[string]int{
		"ingest":  3,
		"retract": 1,
	})

	if audit.entry == nil {
		t.Fatal("expected a wiki activity entry")
	}
	if audit.entry.Action != types.AuditActionWikiContentChanged ||
		audit.entry.ScopeType != auditScopeKnowledgeBase || audit.entry.ScopeID != "kb-1" {
		t.Fatalf("unexpected activity entry: %#v", audit.entry)
	}
	var details map[string]any
	if err := json.Unmarshal(audit.entry.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["count"] != float64(4) {
		t.Fatalf("count = %#v, want 4", details["count"])
	}
	actions, ok := details["actions"].(map[string]any)
	if !ok || actions["ingest"] != float64(3) || actions["retract"] != float64(1) {
		t.Fatalf("actions = %#v", details["actions"])
	}
}

// TestKBActivityTriggerReflectsInitiatorIdentity locks in the worker
// attribution used by ProcessKBClone / ProcessKnowledgeMove /
// ProcessKnowledgeListDelete / ProcessKnowledgeListReparse: the task
// trigger must be derived from the restored initiator, not hard-coded to
// "user". A synthetic (API-key) or empty initiator is service-owned work
// and must therefore report "system", matching the empty actor attribution.
func TestKBActivityTriggerReflectsInitiatorIdentity(t *testing.T) {
	// Real human initiator → "user".
	userCtx := types.TaskInitiator{UserID: "user-1", Role: types.TenantRoleAdmin}.Apply(context.Background())
	if got := kbActivityTrigger(userCtx); got != "user" {
		t.Fatalf("real initiator trigger = %q, want user", got)
	}

	// Empty / legacy initiator (e.g. scheduler or API-key origin) → "system".
	systemCtx := types.TaskInitiator{}.Apply(context.Background())
	if got := kbActivityTrigger(systemCtx); got != "system" {
		t.Fatalf("empty initiator trigger = %q, want system", got)
	}

	// Synthetic API-key user is service identity → "system".
	synthCtx := context.WithValue(context.Background(), types.UserIDContextKey, "system-7")
	if got := kbActivityTrigger(synthCtx); got != "system" {
		t.Fatalf("synthetic user trigger = %q, want system", got)
	}
}
