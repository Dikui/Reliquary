package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"reliquary/internal/types"
)

func (s *Service) SetRecoveryLookup(lookup func(context.Context, *types.IngestFailure, []byte) (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryLookup = lookup
}

func (s *Service) resolveRecovery(ctx context.Context, f *types.IngestFailure) error {
	s.mu.Lock()
	lookup := s.recoveryLookup
	if f.RecoveryEntryID != "" || lookup == nil || !s.now().Before(f.ExpiresAt) {
		s.mu.Unlock()
		return nil
	}
	var input []byte
	var err error
	for _, material := range f.Materials {
		if material.Name != "input.json" || !validID(material.ID) {
			continue
		}
		input, err = os.ReadFile(filepath.Join(s.failureDir(f.ID), material.ID))
		break
	}
	s.mu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || input == nil {
		return err
	}
	// Database lookup must not block capture or retention cleanup.
	entryID, err := lookup(ctx, f, input)
	if err != nil || entryID == "" {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.loadLocked(ctx, f.ID)
	if err != nil {
		return err
	}
	if current.RecoveryEntryID == "" {
		current.RecoveryEntryID = entryID
		if err := s.saveRecoveryLocked(current); err != nil {
			return err
		}
	}
	*f = *current
	return nil
}

func (s *Service) saveRecoveryLocked(f *types.IngestFailure) error {
	f.Resolved = true
	if err := os.MkdirAll(s.failureDir(f.ID), 0700); err != nil {
		return err
	}
	if err := atomicJSON(filepath.Join(s.failureDir(f.ID), "manifest.json"), f); err != nil {
		return err
	}
	// Keep the recovery fact if capacity eviction later removes the manifest.
	summary := *f
	summary.Materials, summary.Config, summary.Missing = nil, nil, nil
	summary.Reason = "Technical ingest failure; inspect diagnostic manifest"
	data, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = s.events.Write(append(data, '\n'))
	return err
}
