package diagnostics

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reliquary/internal/types"
)

// The bounded, content-free event log remains queryable when a package could
// not be saved. A missing manifest never implies its original bytes survived.
func (s *Service) eventsLocked(ctx context.Context) (map[string]types.IngestFailure, error) {
	rows := map[string]types.IngestFailure{}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || (e.Name() != "events.jsonl" && !(strings.HasPrefix(e.Name(), "events-") && strings.HasSuffix(e.Name(), ".jsonl"))) {
			continue
		}
		file, err := os.Open(filepath.Join(s.root, e.Name()))
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 8*1024*1024)
		for scanner.Scan() {
			var f types.IngestFailure
			if json.Unmarshal(scanner.Bytes(), &f) != nil || !validID(f.ID) || space(ctx) == 0 || f.SpaceID != space(ctx) || f.CreatedAt.IsZero() || s.now().Sub(f.CreatedAt) > 28*24*time.Hour {
				continue
			}
			f.PackageStatus = "unavailable"
			f.Reason = "Technical ingest failure; diagnostic package unavailable"
			f.Missing = []string{"diagnostic package was not saved or is no longer retained"}
			f.Materials = []types.FailureMaterial{}
			f.Config = nil
			rows[f.ID] = f
		}
		err = errors.Join(scanner.Err(), file.Close())
		if err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// Manifests override log summaries, including removal and recovery updates.
// Mutations use these raw records, never the masked API response.
func (s *Service) failuresLocked(ctx context.Context) (map[string]types.IngestFailure, error) {
	rows, err := s.eventsLocked(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "failures"))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		f, err := s.manifestLocked(ctx, e.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		rows[f.ID] = *f
	}
	return rows, nil
}
