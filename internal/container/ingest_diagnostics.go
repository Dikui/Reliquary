package container

import (
	"context"
	"fmt"
	"reliquary/internal/config"
	"reliquary/internal/diagnostics"
	"reliquary/internal/handler"
	"reliquary/internal/types/interfaces"
	"time"
)

func newIngestDiagnostics(cfg *config.Config, cleaner interfaces.ResourceCleaner) (*diagnostics.Service, error) {
	c, err := config.ResolveIngestDiagnostics(cfg)
	if err != nil {
		return nil, err
	}
	s, err := diagnostics.New(c.Root, time.Duration(c.RetentionDays)*24*time.Hour, c.MaxBytes)
	if err != nil {
		return nil, err
	}
	s.SetVersion(handler.Version + "@" + handler.CommitID)
	cleaner.RegisterWithName("Ingest diagnostics", s.Start(context.Background()))
	return s, nil
}

func registerIngestDiagnostics(s *diagnostics.Service, entry interfaces.EntryService, h *handler.EntryHandler) error {
	setter, ok := entry.(interface{ SetIngestDiagnostics(*diagnostics.Service) })
	if !ok {
		return fmt.Errorf("entry service does not support ingest diagnostics")
	}
	setter.SetIngestDiagnostics(s)
	h.SetIngestDiagnostics(s)
	return nil
}
