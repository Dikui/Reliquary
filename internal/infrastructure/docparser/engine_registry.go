package docparser

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// EngineRegistration supplies a retained parser's metadata and reader.
type EngineRegistration interface {
	Name() string
	Description() string
	FileTypes() []string
	CheckAvailable(overrides map[string]string) (bool, string)
	NewReader(ctx context.Context, deps ReaderDeps) (interfaces.DocReader, error)
}

type ReaderDeps struct {
	Overrides map[string]string
}

var localEngines []EngineRegistration

func RegisterEngine(e EngineRegistration) { localEngines = append(localEngines, e) }

func lookupEngine(name string) (EngineRegistration, bool) {
	for _, engine := range localEngines {
		if engine.Name() == name {
			return engine, true
		}
	}
	return nil, false
}

// NewReader resolves an explicit parser or the stable default for a file type.
// Missing bindings, unsupported types and removed engines return explicit errors.
func NewReader(ctx context.Context, engine, fileType string, isURL bool, deps ReaderDeps) (interfaces.DocReader, error) {
	fileType = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fileType), "."))
	if engine == "" || engine == "auto" {
		if isURL {
			engine = SimpleEngineName
		} else {
			engine = types.DefaultParserEngine(fileType)
		}
	}
	registration, ok := lookupEngine(engine)
	if !ok {
		return nil, fmt.Errorf("unsupported parser engine %q for file type %q", engine, fileType)
	}
	if isURL {
		if engine != SimpleEngineName {
			return nil, fmt.Errorf("parser engine %q does not support URL imports", engine)
		}
	} else if !slices.Contains(registration.FileTypes(), fileType) {
		return nil, fmt.Errorf("parser engine %q does not support file type %q", engine, fileType)
	}
	return registration.NewReader(ctx, deps)
}

// SupportsFileType reports whether any retained engine can parse this upload.
func SupportsFileType(fileType string) bool {
	fileType = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(fileType), "."))
	for _, engine := range localEngines {
		if slices.Contains(engine.FileTypes(), fileType) {
			return true
		}
	}
	return false
}

func ListAllEngines(overrides map[string]string) []types.ParserEngineInfo {
	result := make([]types.ParserEngineInfo, 0, len(localEngines))
	for _, e := range localEngines {
		available, reason := e.CheckAvailable(overrides)
		result = append(result, types.ParserEngineInfo{Name: e.Name(), Description: e.Description(), FileTypes: e.FileTypes(), Available: available, UnavailableReason: reason})
	}
	return result
}

func errEngineUnavailable(engine, reason string) error {
	return fmt.Errorf("parser engine %q is unavailable: %s", engine, reason)
}
