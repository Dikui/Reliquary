// Package approval implements human-in-the-loop gating for dangerous MCP tool calls (issue #1173).
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"reliquary/internal/config"
	"reliquary/internal/event"
	"reliquary/internal/logger"
)

// Checker answers whether a concrete MCP tool requires human approval before execution.
type Checker interface {
	IsRequired(ctx context.Context, tenantID uint64, serviceID, toolName string) (bool, error)
}

// Decision is the outcome of a pending tool approval.
type Decision struct {
	Approved        bool
	ModifiedArgs    json.RawMessage // optional JSON object; when set and Approved, replaces original args
	Reason          string
	TimedOut        bool
	ContextCanceled bool
}

// PendingRequest carries everything needed to block and notify the UI.
type PendingRequest struct {
	TenantID           uint64
	UserID             string // owner of the session that initiated the call (used for Resolve authorization); empty disables user check
	SessionID          string
	AssistantMessageID string
	RequestID          string
	EventBus           *event.EventBus
	ServiceID          string
	ServiceName        string
	MCPToolName        string // name on MCP server
	RegisteredToolName string // registry name e.g. mcp_svc_tool
	Description        string
	Args               json.RawMessage
	ToolCallID         string
}

// MCPApproval is the surface used by MCPTool (mockable in tests).
type MCPApproval interface {
	NeedsApproval(ctx context.Context, tenantID uint64, serviceID, toolName string) bool
	RequestAndWait(ctx context.Context, req PendingRequest) (Decision, error)
}

// RequiredApproval always asks the initiating user, independent of MCP policy.
type RequiredApproval interface {
	RequireAndWait(ctx context.Context, req PendingRequest) (Decision, error)
}

var _ MCPApproval = (*Gate)(nil)

// Gate coordinates process-local waiters for MCP tool approvals.
type Gate struct {
	mu        sync.Mutex
	pending   map[string]*waiter
	checker   Checker
	timeout   time.Duration
	failClose bool // when true, NeedsApproval errors block (require approval) instead of skip
}

type waiter struct {
	ch       chan Decision
	tenantID uint64
	userID   string // empty means "skip user check"
	once     sync.Once
	// resolved is atomic so deliverLocal can read it without holding g.mu
	// (deliver writes it from timer/ctx branches that don't take g.mu).
	resolved atomic.Bool
}

// deliver returns true when this call won the race and actually delivered the
// decision; false when a previous deliver already happened. Callers can use
// the return value to map to ErrAlreadyResolved for HTTP feedback.
func (w *waiter) deliver(d Decision) bool {
	if w == nil {
		return false
	}
	delivered := false
	w.once.Do(func() {
		select {
		case w.ch <- d:
			w.resolved.Store(true)
			delivered = true
		default:
		}
	})
	return delivered
}

var (
	// ErrPendingNotFound is returned when Resolve is called with an unknown id.
	ErrPendingNotFound = errors.New("tool approval pending not found")
	// ErrTenantMismatch is returned when Resolve tenant does not match the pending request.
	ErrTenantMismatch = errors.New("workspace mismatch for tool approval")
	// ErrAlreadyResolved is returned when Resolve loses the race against a
	// timeout / cancellation: the waiter is still in the map but its decision
	// channel was already consumed by the timer/ctx branch.
	ErrAlreadyResolved = errors.New("tool approval already resolved")
	// ErrUserMismatch is returned when Resolve is invoked by a user that does
	// not own the originating session.
	ErrUserMismatch = errors.New("user mismatch for tool approval")
)

// NewGate builds a gate. checker may be nil (disables gating). cfg may be nil
// (defaults apply).
func NewGate(cfg *config.Config, checker Checker) *Gate {
	timeout := 10 * time.Minute
	if cfg != nil && cfg.Agent != nil && cfg.Agent.ToolApprovalTimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Agent.ToolApprovalTimeoutSeconds) * time.Second
	}
	// Default fail-close: if the checker errors, require approval (safer for a
	// HITL feature). Set RELIQUARY_AGENT_TOOL_APPROVAL_FAIL_OPEN=true to revert.
	failClose := !strings.EqualFold(strings.TrimSpace(os.Getenv("RELIQUARY_AGENT_TOOL_APPROVAL_FAIL_OPEN")), "true")
	g := &Gate{
		pending:   make(map[string]*waiter),
		checker:   checker,
		timeout:   timeout,
		failClose: failClose,
	}
	return g
}

// NeedsApproval returns whether execution should pause for human confirmation.
func (g *Gate) NeedsApproval(ctx context.Context, tenantID uint64, serviceID, toolName string) bool {
	if g == nil || g.checker == nil || tenantID == 0 || serviceID == "" || toolName == "" {
		return false
	}
	ok, err := g.checker.IsRequired(ctx, tenantID, serviceID, toolName)
	if err != nil {
		// Default fail-close: a transient DB error must NOT silently allow a
		// dangerous tool to run. Operators can opt into legacy behaviour via
		// RELIQUARY_AGENT_TOOL_APPROVAL_FAIL_OPEN=true.
		if g.failClose {
			logger.GetLogger(ctx).Warnf("mcp tool approval check failed (fail-close: requiring approval): %v", err)
			return true
		}
		logger.GetLogger(ctx).Warnf("mcp tool approval check failed (fail-open: skip gate): %v", err)
		return false
	}
	return ok
}

// RequestAndWait emits a UI event, then blocks until Resolve, timeout, or ctx cancellation.
func (g *Gate) RequestAndWait(ctx context.Context, req PendingRequest) (Decision, error) {
	if g == nil {
		return Decision{Approved: true}, nil
	}
	if g.checker == nil {
		return Decision{Approved: true}, nil
	}
	return g.RequireAndWait(ctx, req)
}

func (g *Gate) RequireAndWait(ctx context.Context, req PendingRequest) (Decision, error) {
	if g == nil {
		return Decision{}, fmt.Errorf("tool approval gate unavailable")
	}
	if req.EventBus == nil {
		return Decision{}, fmt.Errorf("tool approval: EventBus is nil")
	}

	pendingID := uuid.New().String()
	w := &waiter{
		ch:       make(chan Decision, 1),
		tenantID: req.TenantID,
		userID:   req.UserID,
	}

	g.mu.Lock()
	g.pending[pendingID] = w
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.pending, pendingID)
		g.mu.Unlock()
	}()

	var argsObj interface{}
	if len(req.Args) > 0 {
		_ = json.Unmarshal(req.Args, &argsObj)
	}

	timeoutSec := int(g.timeout / time.Second)
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	evtData := event.ToolApprovalRequiredData{
		PendingID:          pendingID,
		TenantID:           req.TenantID,
		SessionID:          req.SessionID,
		AssistantMessageID: req.AssistantMessageID,
		ServiceID:          req.ServiceID,
		ServiceName:        req.ServiceName,
		MCPToolName:        req.MCPToolName,
		RegisteredToolName: req.RegisteredToolName,
		Description:        req.Description,
		Args:               argsObj,
		ArgsJSON:           string(req.Args),
		TimeoutSeconds:     timeoutSec,
		RequestedAtUnix:    time.Now().Unix(),
		ToolCallID:         req.ToolCallID,
		RequestID:          req.RequestID,
	}

	if err := req.EventBus.Emit(ctx, event.Event{
		ID:        pendingID + "-approval-required",
		Type:      event.EventToolApprovalRequired,
		SessionID: req.SessionID,
		Data:      evtData,
		Metadata: map[string]interface{}{
			"assistant_message_id": req.AssistantMessageID,
			"pending_id":           pendingID,
		},
		RequestID: req.RequestID,
	}); err != nil {
		return Decision{}, fmt.Errorf("emit tool approval required: %w", err)
	}

	timer := time.NewTimer(g.timeout)
	defer timer.Stop()

	emitResolved := func(d Decision) {
		_ = req.EventBus.Emit(context.WithoutCancel(ctx), event.Event{
			ID:        pendingID + "-approval-resolved",
			Type:      event.EventToolApprovalResolved,
			SessionID: req.SessionID,
			Data: event.ToolApprovalResolvedData{
				PendingID: pendingID,
				Approved:  d.Approved,
				Reason:    d.Reason,
				TimedOut:  d.TimedOut,
				Canceled:  d.ContextCanceled,
			},
			Metadata: map[string]interface{}{
				"assistant_message_id": req.AssistantMessageID,
			},
			RequestID: req.RequestID,
		})
	}

	var d Decision
	select {
	case d = <-w.ch:
		emitResolved(d)
		return d, nil
	case <-timer.C:
		d = Decision{Approved: false, Reason: "approval timeout", TimedOut: true}
		_ = w.deliver(d)
		d = <-w.ch
		emitResolved(d)
		return d, nil
	case <-ctx.Done():
		d = Decision{Approved: false, Reason: "request canceled", ContextCanceled: true}
		_ = w.deliver(d)
		d = <-w.ch
		emitResolved(d)
		return d, nil
	}
}

// Resolve completes a pending approval after checking its tenant and user.
func (g *Gate) Resolve(tenantID uint64, userID, pendingID string, d Decision) error {
	if g == nil {
		return fmt.Errorf("gate is nil")
	}
	return g.deliverLocal(tenantID, userID, pendingID, d)
}

// deliverLocal attempts to satisfy a waiter on this instance only.
func (g *Gate) deliverLocal(tenantID uint64, userID, pendingID string, d Decision) error {
	g.mu.Lock()
	w, ok := g.pending[pendingID]
	if !ok {
		g.mu.Unlock()
		return ErrPendingNotFound
	}
	if w.tenantID != tenantID {
		g.mu.Unlock()
		return ErrTenantMismatch
	}
	// Authorization: when the pending was registered with a userID, the
	// caller MUST present the same non-empty userID. An empty caller
	// userID is treated as a mismatch (fail-close) so a missing auth
	// middleware cannot bypass the per-user check.
	if w.userID != "" && w.userID != userID {
		g.mu.Unlock()
		return ErrUserMismatch
	}
	g.mu.Unlock()
	already := w.resolved.Load()

	if already {
		return ErrAlreadyResolved
	}
	if !w.deliver(d) {
		return ErrAlreadyResolved
	}
	return nil
}

// Adapter makes MCPToolApprovalService satisfy Checker without importing the service package here.
type Adapter struct {
	Svc interface {
		IsRequired(ctx context.Context, tenantID uint64, serviceID, toolName string) (bool, error)
	}
}

// IsRequired implements Checker.
func (a *Adapter) IsRequired(ctx context.Context, tenantID uint64, serviceID, toolName string) (bool, error) {
	if a == nil || a.Svc == nil {
		return false, nil
	}
	return a.Svc.IsRequired(ctx, tenantID, serviceID, toolName)
}
