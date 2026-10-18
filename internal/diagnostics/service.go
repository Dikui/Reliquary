// Package diagnostics retains bounded, short-lived ingest failure materials independently of business transactions.
package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/natefinch/lumberjack.v2"
	"reliquary/internal/types"
)

const DefaultRetention = 7 * 24 * time.Hour
const DefaultMaxBytes int64 = 512 * 1024 * 1024

var ErrNotFound = errors.New("diagnostic material not found")
var ErrExpired = errors.New("diagnostic material expired")

type Service struct {
	root      string
	retention time.Duration
	maxBytes  int64
	mu        sync.Mutex
	version   string
	active    map[string]*Attempt
	now       func() time.Time
	events    *lumberjack.Logger
}

func New(root string, retention time.Duration, maxBytes int64) (*Service, error) {
	if !filepath.IsAbs(root) || retention <= 0 || maxBytes <= 0 {
		return nil, fmt.Errorf("diagnostics requires an absolute directory and positive limits")
	}
	for _, name := range []string{"failures", "temporary"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			return nil, err
		}
	}
	s := &Service{root: root, retention: retention, maxBytes: maxBytes, now: time.Now, active: map[string]*Attempt{},
		events: &lumberjack.Logger{Filename: filepath.Join(root, "events.jsonl"), MaxSize: 8, MaxBackups: 2, MaxAge: 28}}
	// Temporary captures cannot be resumed after a process restart.
	entries, err := os.ReadDir(filepath.Join(root, "temporary"))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if _, err := uuid.Parse(e.Name()); err == nil {
			if err := os.RemoveAll(filepath.Join(root, "temporary", e.Name())); err != nil {
				return nil, err
			}
		}
	}
	return s, s.Prune()
}

func (s *Service) SetVersion(version string) { s.mu.Lock(); defer s.mu.Unlock(); s.version = version }

// Start uses a bounded local sweep; database outages cannot suspend retention.
func (s *Service) Start(ctx context.Context) func() error {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = s.Prune()
			}
		}
	}()
	return func() error { cancel(); <-done; return s.events.Close() }
}

func atomicJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomicBytes(path, data)
}

func atomicBytes(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func space(ctx context.Context) uint64         { id, _ := types.SpaceIDFromContext(ctx); return id }
func validID(id string) bool                   { _, err := uuid.Parse(id); return err == nil && filepath.Base(id) == id }
func (s *Service) failureDir(id string) string { return filepath.Join(s.root, "failures", id) }

// Load the stored manifest before applying any response-only expiry masking.
func (s *Service) manifestLocked(ctx context.Context, id string) (*types.IngestFailure, error) {
	if !validID(id) || space(ctx) == 0 {
		return nil, ErrNotFound
	}
	data, err := os.ReadFile(filepath.Join(s.failureDir(id), "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var f types.IngestFailure
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.SpaceID != space(ctx) || f.ID != id {
		return nil, ErrNotFound
	}
	return &f, nil
}

func (s *Service) loadLocked(ctx context.Context, id string) (*types.IngestFailure, error) {
	f, err := s.manifestLocked(ctx, id)
	if !errors.Is(err, ErrNotFound) || !validID(id) || space(ctx) == 0 {
		return f, err
	}
	events, err := s.eventsLocked(ctx)
	if err != nil {
		return nil, err
	}
	if f, ok := events[id]; ok {
		return &f, nil
	}
	return nil, ErrNotFound
}

func (s *Service) viewLocked(f *types.IngestFailure) *types.IngestFailure {
	if f.PackageStatus == "available" || f.PackageStatus == "partial" {
		for _, m := range f.Materials {
			info, err := os.Stat(filepath.Join(s.failureDir(f.ID), m.ID))
			if !validID(m.ID) || err != nil || info.Size() != m.Size {
				f.PackageStatus = "partial"
				f.Missing = append(f.Missing, "a captured material is missing or incomplete")
				break
			}
		}
	}
	if !s.now().Before(f.ExpiresAt) || f.PackageStatus == "expired" {
		if f.PackageStatus != "unavailable" {
			f.PackageStatus = "expired"
		}
		f.Config = nil
		f.Reason = "Diagnostic materials expired"
		f.Missing = []string{"diagnostic retention or capacity limit reached"}
		f.Materials = []types.FailureMaterial{}
	}
	return f
}

func (s *Service) getLocked(ctx context.Context, id string) (*types.IngestFailure, error) {
	f, err := s.loadLocked(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.viewLocked(f), nil
}

func (s *Service) Get(ctx context.Context, id string) (*types.IngestFailure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(ctx, id)
}

func (s *Service) List(ctx context.Context, q types.FailureQuery) (*types.FailurePage, error) {
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	if q.Page < 1 || q.PageSize < 1 || q.PageSize > 100 || (q.From != nil && q.To != nil && q.From.After(*q.To)) {
		return nil, fmt.Errorf("invalid diagnostic pagination or time range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	failures, err := s.failuresLocked(ctx)
	if err != nil {
		return nil, err
	}
	rows := []types.IngestFailure{}
	for _, stored := range failures {
		f := s.viewLocked(&stored)
		if q.ID != "" && q.ID != f.ID || q.RequestID != "" && q.RequestID != f.RequestID || q.APIKeyID != "" && q.APIKeyID != f.APIKeyID || q.LibraryID != "" && q.LibraryID != f.LibraryID || q.ErrorCode != "" && q.ErrorCode != f.ErrorCode || q.From != nil && f.CreatedAt.Before(*q.From) || q.To != nil && f.CreatedAt.After(*q.To) {
			continue
		}
		f.Materials = nil
		f.Config = nil
		rows = append(rows, *f)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	start := len(rows)
	if q.Page-1 <= len(rows)/q.PageSize {
		start = min((q.Page-1)*q.PageSize, len(rows))
	}
	end := min(start+q.PageSize, len(rows))
	return &types.FailurePage{Items: rows[start:end], Total: len(rows), Page: q.Page, PageSize: q.PageSize}, nil
}

func (s *Service) Read(ctx context.Context, id, materialID string) (types.FailureMaterial, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.getLocked(ctx, id)
	if err != nil {
		return types.FailureMaterial{}, nil, err
	}
	if f.PackageStatus == "expired" {
		return types.FailureMaterial{}, nil, ErrExpired
	}
	if f.PackageStatus == "unavailable" || !validID(materialID) {
		return types.FailureMaterial{}, nil, ErrNotFound
	}
	for _, m := range f.Materials {
		if m.ID != materialID {
			continue
		}
		root, err := os.OpenRoot(s.failureDir(id))
		if err != nil {
			return m, nil, err
		}
		defer root.Close()
		file, err := root.Open(materialID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return m, nil, ErrNotFound
			}
			return m, nil, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, m.Size+1))
		if err != nil {
			return m, nil, err
		}
		hash := sha256.Sum256(data)
		if int64(len(data)) != m.Size || hex.EncodeToString(hash[:]) != m.SHA256 {
			return m, nil, fmt.Errorf("diagnostic material integrity check failed")
		}
		return m, data, nil
	}
	return types.FailureMaterial{}, nil, ErrNotFound
}

func (s *Service) ReadText(ctx context.Context, id, materialID string, offset, length int) (*types.FailureMaterialRead, error) {
	if offset < 0 || length < 0 || length > 100000 {
		return nil, fmt.Errorf("offset must be nonnegative and length at most 100000")
	}
	if length == 0 {
		length = 20000
	}
	m, data, err := s.Read(ctx, id, materialID)
	if err != nil {
		return nil, err
	}
	r := &types.FailureMaterialRead{Material: m, Offset: offset}
	if strings.HasPrefix(m.Type, "image/") {
		r.URL = "/api/v1/diagnostics/ingest-failures/" + id + "/materials/" + materialID
		r.Data = data
		return r, nil
	}
	runes := []rune(string(data))
	if offset > len(runes) {
		return nil, fmt.Errorf("offset exceeds material length")
	}
	end := min(offset+length, len(runes))
	r.Content = string(runes[offset:end])
	r.Length = end - offset
	r.TotalLength = len(runes)
	if end < len(runes) {
		r.NextOffset = &end
	}
	return r, nil
}

func (s *Service) LinkRecovery(ctx context.Context, id, entryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked(ctx, id)
	if err != nil {
		return err
	}
	f.RecoveryEntryID = entryID
	f.RecoveryRequestID, _ = ctx.Value(types.RequestIDContextKey).(string)
	if err := os.MkdirAll(s.failureDir(id), 0700); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(s.failureDir(id), "manifest.json"), f)
}

func (s *Service) RemoveFailureMaterials(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.loadLocked(ctx, id); err != nil {
		return err
	}
	return s.removeRelatedLocked(ctx, "", id)
}

func (s *Service) rejectLocked(f *types.IngestFailure) error {
	if _, err := os.Stat(s.failureDir(f.ID)); errors.Is(err, os.ErrNotExist) {
		return nil // Log-only failures have no captured content to remove.
	} else if err != nil {
		return err
	}
	f.PackageStatus = "unavailable"
	f.Missing = []string{"materials removed after confirmed rejection"}
	f.Config = nil
	f.Reason = "Materials removed after confirmed rejection"
	if err := atomicJSON(filepath.Join(s.failureDir(f.ID), "manifest.json"), f); err != nil {
		return err
	}
	return s.removeMaterials(f)
}

// RemoveEntryMaterials keeps only content-free diagnostic metadata after a confirmed rejection.
func (s *Service) RemoveEntryMaterials(ctx context.Context, entryID, recoveryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removeRelatedLocked(ctx, entryID, recoveryID)
}

func (s *Service) removeRelatedLocked(ctx context.Context, entryID, recoveryID string) error {
	failures, err := s.failuresLocked(ctx)
	if err != nil {
		return err
	}
	for id, a := range s.active {
		if a.failure.SpaceID == space(ctx) {
			failures[id] = a.failure
		}
	}
	selected := map[string]bool{}
	if recoveryID != "" {
		selected[recoveryID] = true
	}
	for id, f := range failures {
		if entryID != "" && (f.EntryID == entryID || f.RecoveryEntryID == entryID) {
			selected[id] = true
		}
	}
	// A failed repair may itself be repaired. Remove its ancestors and siblings,
	// even when the original failure never obtained a business entry ID.
	for changed := true; changed; {
		changed = false
		for id, f := range failures {
			parent := f.RecoveryFailureID
			if parent != "" && (selected[id] || selected[parent]) {
				if !selected[id] || !selected[parent] {
					changed = true
				}
				selected[id], selected[parent] = true, true
			}
		}
	}
	for id := range selected {
		if a := s.active[id]; a != nil && a.failure.SpaceID == space(ctx) {
			if err := a.suppressLocked(); err != nil {
				return err
			}
			continue
		}
		f, ok := failures[id]
		if !ok {
			continue
		}
		if err := s.rejectLocked(&f); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) removeMaterials(f *types.IngestFailure) error {
	entries, err := os.ReadDir(s.failureDir(f.ID))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == "manifest.json" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.failureDir(f.ID), e.Name())); err != nil {
			return err
		}
	}
	f.Materials = []types.FailureMaterial{}
	return atomicJSON(filepath.Join(s.failureDir(f.ID), "manifest.json"), f)
}

func directorySize(path string) int64 {
	var n int64
	_ = filepath.WalkDir(path, func(_ string, e os.DirEntry, err error) error {
		if err == nil && e.Type().IsRegular() {
			if info, err := e.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func (s *Service) Prune() error       { s.mu.Lock(); defer s.mu.Unlock(); return s.pruneLocked() }
func (s *Service) pruneLocked() error { return s.pruneForLocked(0) }
func (s *Service) pruneForLocked(reserve int64) error {
	temporary, err := os.ReadDir(filepath.Join(s.root, "temporary"))
	if err != nil {
		return err
	}
	for _, e := range temporary {
		if validID(e.Name()) && s.active[e.Name()] == nil {
			if err := os.RemoveAll(filepath.Join(s.root, "temporary", e.Name())); err != nil {
				return err
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "failures"))
	if err != nil {
		return err
	}
	rows := []types.IngestFailure{}
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.failureDir(e.Name()), "manifest.json"))
		var f types.IngestFailure
		if err != nil || json.Unmarshal(data, &f) != nil || f.ID != e.Name() {
			// A package without a valid manifest cannot be read or recovered.
			// Do not let an interrupted write suspend every other package's expiry.
			if err := os.RemoveAll(s.failureDir(e.Name())); err != nil {
				return err
			}
			continue
		}
		if s.now().Sub(f.CreatedAt) > 28*24*time.Hour {
			if err := os.RemoveAll(s.failureDir(f.ID)); err != nil {
				return err
			}
			continue
		}
		if f.PackageStatus == "unavailable" && len(f.Materials) > 0 {
			if err := s.removeMaterials(&f); err != nil {
				return err
			}
		}
		if !s.now().Before(f.ExpiresAt) {
			if f.PackageStatus != "unavailable" {
				f.PackageStatus = "expired"
			}
			f.Config = nil
			f.Reason = "Diagnostic materials expired"
			f.Missing = []string{"diagnostic retention limit reached"}
			if err := s.removeMaterials(&f); err != nil {
				return err
			}
		}
		rows = append(rows, f)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	total := directorySize(filepath.Join(s.root, "failures")) + directorySize(filepath.Join(s.root, "temporary"))
	for i := range rows {
		if total+reserve <= s.maxBytes {
			break
		}
		f := &rows[i]
		if len(f.Materials) == 0 {
			continue
		}
		before := directorySize(s.failureDir(f.ID))
		f.PackageStatus = "expired"
		f.Missing = append(f.Missing, "evicted by diagnostic capacity limit")
		f.Config = nil
		f.Reason = "Diagnostic materials expired"
		if err := s.removeMaterials(f); err != nil {
			return err
		}
		total -= before - directorySize(s.failureDir(f.ID))
	}
	// Metadata is also bounded. At extreme failure volumes, discard the oldest
	// content-free records after all removable packages have been evicted.
	for i := range rows {
		if total+reserve <= s.maxBytes {
			break
		}
		path := s.failureDir(rows[i].ID)
		n := directorySize(path)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		total -= n
	}
	return nil
}
