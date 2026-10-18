package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"reliquary/internal/types"
)

type attemptKey struct{}
type transportKey struct{}

type Attempt struct {
	service    *Service
	dir        string
	failure    types.IngestFailure
	secrets    []string
	calls      []*types.ModelCall
	suppressed bool
}

func WithTransport(ctx context.Context, transport string) context.Context {
	return context.WithValue(ctx, transportKey{}, transport)
}
func FromContext(ctx context.Context) *Attempt { a, _ := ctx.Value(attemptKey{}).(*Attempt); return a }

func (s *Service) Begin(ctx context.Context, input []byte, index int, library, entryID string) (context.Context, *Attempt) {
	a := &Attempt{service: s}
	now := s.now().UTC()
	requestID, _ := ctx.Value(types.RequestIDContextKey).(string)
	transport, _ := ctx.Value(transportKey{}).(string)
	if transport == "" {
		transport = "http"
	}
	a.failure = types.IngestFailure{ID: uuid.NewString(), SpaceID: space(ctx), RequestID: requestID, CreatedAt: now, ExpiresAt: now.Add(s.retention), Transport: transport, ItemIndex: index, Library: library, EntryID: entryID, Stage: "ingest", Outcome: "rolled_back", Missing: []string{}, Materials: []types.FailureMaterial{}, Config: map[string]any{}, Version: os.Getenv("RELIQUARY_COMMIT")}
	if p, ok := types.PrincipalFromContext(ctx); ok {
		a.failure.APIKeyID = p.ID
		a.failure.APIKeyName = p.Name
	}
	s.mu.Lock()
	s.active[a.failure.ID] = a
	if s.version != "" {
		a.failure.Version = s.version
	}
	if a.failure.Version == "" || a.failure.Version == "unknown@unknown" {
		a.failure.Missing = append(a.failure.Missing, "program version unavailable")
	}
	a.dir = filepath.Join(s.root, "temporary", a.failure.ID)
	if err := os.Mkdir(a.dir, 0700); err != nil {
		a.dir = ""
		a.failure.Missing = append(a.failure.Missing, "temporary capture unavailable")
	}
	s.mu.Unlock()
	if input != nil {
		a.Add("input.json", "application/json", "request", input)
	}
	return context.WithValue(ctx, attemptKey{}, a), a
}

func (a *Attempt) Add(name, kind, source string, data []byte) {
	if a == nil {
		return
	}
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.dir == "" || a.suppressed {
		return
	}
	hash := sha256.Sum256(data)
	sum := hex.EncodeToString(hash[:])
	for _, m := range a.failure.Materials {
		if m.SHA256 == sum && m.Type == kind && m.Source == source {
			return
		}
	}
	if int64(len(data))+8192 <= s.maxBytes {
		_ = s.pruneForLocked(int64(len(data)) + 8192)
	}
	if directorySize(filepath.Join(s.root, "temporary"))+directorySize(filepath.Join(s.root, "failures"))+int64(len(data))+8192 > s.maxBytes {
		a.failure.Missing = append(a.failure.Missing, "capture exceeds capacity: "+name)
		return
	}
	id := uuid.NewString()
	path := filepath.Join(a.dir, id)
	if err := atomicBytes(path, data); err != nil {
		a.failure.Missing = append(a.failure.Missing, "capture write failed: "+name)
		return
	}
	a.failure.Materials = append(a.failure.Materials, types.FailureMaterial{ID: id, Name: filepath.Base(name), Type: kind, Source: source, Size: int64(len(data)), SHA256: sum})
}

func CaptureImage(ctx context.Context, name, source, kind string, data []byte) {
	if a := FromContext(ctx); a != nil {
		a.Add(name, kind, source, data)
	}
}
func SetStage(ctx context.Context, stage string) {
	if a := FromContext(ctx); a != nil {
		a.service.mu.Lock()
		defer a.service.mu.Unlock()
		a.failure.Stage = stage
	}
}
func SetOutcome(ctx context.Context, outcome, entryID string) {
	if a := FromContext(ctx); a != nil {
		a.service.mu.Lock()
		defer a.service.mu.Unlock()
		a.failure.Outcome = outcome
		if entryID != "" {
			a.failure.EntryID = entryID
		}
	}
}
func SetLibrary(ctx context.Context, library *types.Library) {
	if a := FromContext(ctx); a != nil && library != nil {
		a.service.mu.Lock()
		defer a.service.mu.Unlock()
		if a.suppressed {
			return
		}
		a.failure.LibraryID = library.ID
		a.failure.Config["library_template"] = library.Template
		a.failure.Config["review_criteria"] = library.ReviewCriteria
		a.failure.Config["source_evidence_rule"] = library.SourceEvidenceRule
		a.failure.Config["model_tiers"] = library.ModelTiers
	}
}
func (a *Attempt) AddSecrets(values ...string) {
	if a != nil {
		for _, v := range values {
			if v != "" {
				a.secrets = append(a.secrets, v)
			}
		}
	}
}
func (a *Attempt) SetConfig(key string, value any) {
	if a != nil {
		a.service.mu.Lock()
		defer a.service.mu.Unlock()
		if a.suppressed {
			return
		}
		a.failure.Config[key] = value
	}
}

func (a *Attempt) SetRecoveryFailure(id string) {
	a.service.mu.Lock()
	defer a.service.mu.Unlock()
	a.failure.RecoveryFailureID = id
}

// Keep in-flight repairs from recreating content after its confirmed removal.
func (a *Attempt) suppressLocked() error {
	a.suppressed = true
	a.failure.Materials = []types.FailureMaterial{}
	a.failure.Config = nil
	if a.dir != "" {
		if err := os.RemoveAll(a.dir); err != nil {
			return err
		}
	}
	a.dir = ""
	return nil
}

func (a *Attempt) RequireImage(source string) {
	a.service.mu.Lock()
	defer a.service.mu.Unlock()
	for _, m := range a.failure.Materials {
		if m.Source == source {
			return
		}
	}
	a.failure.Missing = append(a.failure.Missing, "image bytes unavailable: "+source)
}
func (a *Attempt) ModelCall(call *types.ModelCall) {
	if a == nil || call == nil {
		return
	}
	copy := *call
	a.service.mu.Lock()
	a.calls = append(a.calls, &copy)
	if call.Content != nil {
		if !call.Content.Complete {
			a.failure.Missing = append(a.failure.Missing, "model response incomplete")
		}
		a.service.mu.Unlock()
		data, _ := json.Marshal(struct {
			Call    *types.ModelCall        `json:"call"`
			Content *types.ModelCallContent `json:"content"`
		}{call, call.Content})
		a.Add("model-call-"+call.ID+".json", "application/json", "model_call", data)
	} else {
		a.failure.Missing = append(a.failure.Missing, "model call content unavailable")
		a.service.mu.Unlock()
	}
}

// Model bodies may enter the ledger only after the business mutation succeeds.
func (a *Attempt) DrainModelCalls(keepContent bool) []*types.ModelCall {
	a.service.mu.Lock()
	defer a.service.mu.Unlock()
	calls := a.calls
	a.calls = nil
	if !keepContent {
		for _, call := range calls {
			call.Content = nil
		}
	}
	return calls
}

func (a *Attempt) Discard() error {
	if a == nil {
		return nil
	}
	a.service.mu.Lock()
	defer a.service.mu.Unlock()
	delete(a.service.active, a.failure.ID)
	if a.dir == "" {
		return nil
	}
	return os.RemoveAll(a.dir)
}

func (a *Attempt) Record(err error, code string, retryable bool, outcome string) *types.IngestDiagnosticReference {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, a.failure.ID)
	a.failure.ErrorCode = code
	a.failure.Retryable = retryable
	if outcome != "" {
		a.failure.Outcome = outcome
	}
	reason := err.Error()
	for _, secret := range a.secrets {
		reason = strings.ReplaceAll(reason, secret, "[REDACTED]")
	}
	if len(reason) > 2048 {
		reason = reason[:2048]
	}
	a.failure.Reason = reason
	a.failure.PackageStatus = "available"
	if len(a.failure.Missing) > 0 {
		a.failure.PackageStatus = "partial"
	}
	if len(a.failure.Materials) == 0 {
		a.failure.PackageStatus = "unavailable"
	}
	// Begin and SetRecoveryFailure are separate calls. Also check the persisted
	// parent so a rejection in that small interval cannot recreate a package.
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, a.failure.SpaceID)
	var cleanupErr error
	suppressionReason := "Materials removed after confirmed rejection"
	seen := map[string]bool{}
	for id := a.failure.RecoveryFailureID; id != "" && !seen[id] && !a.suppressed; {
		seen[id] = true
		parent, e := s.manifestLocked(ctx, id)
		if e != nil {
			// If the parent marker cannot be checked, retain only a summary.
			// Missing or unreadable metadata must not undo a prior removal.
			cleanupErr = a.suppressLocked()
			suppressionReason = "Recovery materials no longer retained"
			break
		}
		if parent.PackageStatus == "unavailable" && parent.Reason == "Materials removed after confirmed rejection" {
			cleanupErr = a.suppressLocked()
			break
		}
		id = parent.RecoveryFailureID
	}
	if a.suppressed {
		if cleanupErr == nil {
			cleanupErr = a.suppressLocked()
		}
		a.failure.PackageStatus = "unavailable"
		a.failure.Reason = suppressionReason
		a.failure.Missing = []string{suppressionReason}
	}
	ref := &types.IngestDiagnosticReference{FailureID: a.failure.ID, PackageStatus: a.failure.PackageStatus}
	dir := s.failureDir(a.failure.ID)
	saveErr := cleanupErr
	if saveErr == nil && a.dir != "" {
		saveErr = atomicJSON(filepath.Join(a.dir, "manifest.json"), &a.failure)
		if saveErr == nil {
			saveErr = os.Rename(a.dir, dir)
		}
	} else if saveErr == nil {
		saveErr = os.Mkdir(dir, 0700)
		if saveErr == nil {
			saveErr = atomicJSON(filepath.Join(dir, "manifest.json"), &a.failure)
		}
	}
	if saveErr == nil {
		ref.Recorded = true
		_ = s.pruneLocked()
		if f, e := s.getLocked(context.WithValue(context.Background(), types.SpaceIDContextKey, a.failure.SpaceID), a.failure.ID); e == nil {
			ref.PackageStatus = f.PackageStatus
		}
	} else {
		ref.PackageStatus = "unavailable"
		_ = os.RemoveAll(dir)
		if a.dir != "" {
			_ = os.RemoveAll(a.dir)
		}
	}
	// The shared log contains only identifiers and categories, never raw request or provider error text.
	event := map[string]any{
		"id": a.failure.ID, "request_id": a.failure.RequestID, "created_at": a.failure.CreatedAt,
		"expires_at": a.failure.ExpiresAt, "space_id": a.failure.SpaceID,
		"transport": a.failure.Transport, "api_key_id": a.failure.APIKeyID, "api_key_name": a.failure.APIKeyName,
		"library_id": a.failure.LibraryID, "item_index": a.failure.ItemIndex, "entry_id": a.failure.EntryID,
		"recovery_failure_id": a.failure.RecoveryFailureID,
		"error_code":          code, "reason": "Technical ingest failure; inspect diagnostic manifest",
		"stage": a.failure.Stage, "outcome": a.failure.Outcome, "retryable": retryable,
		"recorded": ref.Recorded, "package_status": ref.PackageStatus, "missing_count": len(a.failure.Missing),
	}
	data, _ := json.Marshal(event)
	_, _ = s.events.Write(append(data, '\n'))
	return ref
}

func (s *Service) RecordUploadFailure(ctx context.Context, name string, data []byte, err error, complete bool) *types.IngestDiagnosticReference {
	_, a := s.Begin(ctx, nil, -1, "", "")
	s.mu.Lock()
	a.failure.Stage = "asset_upload"
	// An upload has no JSON request body; its original bytes are the input.
	if !complete {
		a.failure.Missing = append(a.failure.Missing, "uploaded image bytes incomplete")
	}
	s.mu.Unlock()
	if len(data) > 0 {
		kind := http.DetectContentType(data)
		if !complete && !strings.HasPrefix(kind, "image/") {
			if expected := mime.TypeByExtension(filepath.Ext(name)); strings.HasPrefix(expected, "image/") {
				kind = expected
			}
		}
		a.Add(name, kind, "upload", data)
	}
	outcome, retryable := "rolled_back", false
	var network net.Error
	if errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		outcome, retryable = "unknown", true
	}
	return a.Record(err, "ingest_asset_failed", retryable, outcome)
}

func (a *Attempt) Panic(v any) {
	a.Record(fmt.Errorf("ingest panic: %v", v), "ingest_panic", false, "unknown")
}
