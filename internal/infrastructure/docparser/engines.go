package docparser

import (
	"context"
	"strings"

	"reliquary/internal/infrastructure/docparser/anydoc"
	"reliquary/internal/types/interfaces"
)

// Engine names, as stored in knowledge base parser rules and shown in the UI.
const (
	// SimpleEngineName is Go-native handling of text formats and images.
	SimpleEngineName = "simple"
	// AnydocEngineName is the in-process anydoc office-document converter.
	AnydocEngineName = "anydoc"
	MinerUEngineName = "mineru"
	// MinerUCloudEngineName is the MinerU Cloud API.
	MinerUCloudEngineName = "mineru_cloud"
)

func init() {
	RegisterEngine(&simpleEngine{})
	RegisterEngine(&anydocEngine{})
	RegisterEngine(&mineruEngine{})
	RegisterEngine(&mineruCloudEngine{})
}

// simple parses text, images and audio in this process.
type simpleEngine struct{}

func (e *simpleEngine) Name() string { return SimpleEngineName }

func (e *simpleEngine) Description() string {
	return "Simple format & image parsing (no external service required)"
}

func (e *simpleEngine) FileTypes() []string {
	return []string{
		"md", "markdown", "txt", "text", "csv", "json",
		"jpg", "jpeg", "png", "gif", "bmp", "tiff", "webp",
		"mp3", "wav", "m4a", "flac", "ogg",
	}
}

func (e *simpleEngine) CheckAvailable(_ map[string]string) (bool, string) {
	return true, ""
}

func (e *simpleEngine) NewReader(_ context.Context, _ ReaderDeps) (interfaces.DocReader, error) {
	return &SimpleFormatReader{}, nil
}

// ---------------------------------------------------------------------------
// anydoc — office documents converted in this process, no external service.
// Only present in builds that link the converter (see the anydoc package).
// ---------------------------------------------------------------------------

type anydocEngine struct{}

func (e *anydocEngine) Name() string { return AnydocEngineName }

func (e *anydocEngine) Description() string {
	return "anydoc in-process office document converter (no external service required)"
}

func (e *anydocEngine) FileTypes() []string { return anydoc.SupportedFileTypes() }

func (e *anydocEngine) CheckAvailable(_ map[string]string) (bool, string) {
	if anydoc.Available() {
		return true, ""
	}
	return false, anydoc.UnavailableReason()
}

func (e *anydocEngine) NewReader(_ context.Context, deps ReaderDeps) (interfaces.DocReader, error) {
	if !anydoc.Available() {
		return nil, errEngineUnavailable(AnydocEngineName, anydoc.UnavailableReason())
	}
	var fallback interfaces.DocReader
	if strings.TrimSpace(deps.Overrides["mineru_endpoint"]) != "" {
		fallback = NewMinerUReader(deps.Overrides)
	} else if strings.TrimSpace(deps.Overrides["mineru_api_key"]) != "" {
		fallback = NewMinerUCloudReader(deps.Overrides)
	}
	return NewAnydocReader(deps.Overrides, fallback), nil
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// mineru — Go-native, calls self-hosted MinerU API directly
// ---------------------------------------------------------------------------

type mineruEngine struct{}

func (e *mineruEngine) Name() string { return MinerUEngineName }

func (e *mineruEngine) Description() string { return "MinerU self-hosted service" }

func (e *mineruEngine) FileTypes() []string {
	return []string{"pdf", "jpg", "jpeg", "png", "bmp", "tiff", "doc", "docx", "ppt", "pptx"}
}

func (e *mineruEngine) CheckAvailable(overrides map[string]string) (bool, string) {
	endpoint := strings.TrimSpace(overrides["mineru_endpoint"])
	if endpoint == "" {
		return false, "MinerU service not configured"
	}
	return PingMinerU(endpoint)
}

func (e *mineruEngine) NewReader(_ context.Context, deps ReaderDeps) (interfaces.DocReader, error) {
	return NewMinerUReader(deps.Overrides), nil
}

// ---------------------------------------------------------------------------
// mineru_cloud — Go-native, calls MinerU Cloud API directly
// ---------------------------------------------------------------------------

type mineruCloudEngine struct{}

func (e *mineruCloudEngine) Name() string { return MinerUCloudEngineName }

func (e *mineruCloudEngine) Description() string { return "MinerU Cloud API" }

func (e *mineruCloudEngine) FileTypes() []string {
	return []string{"pdf", "jpg", "jpeg", "png", "bmp", "tiff", "doc", "docx", "ppt", "pptx"}
}

func (e *mineruCloudEngine) CheckAvailable(overrides map[string]string) (bool, string) {
	apiKey := strings.TrimSpace(overrides["mineru_api_key"])
	if apiKey == "" {
		return false, "MinerU API Key not configured"
	}
	return PingMinerUCloud(apiKey)
}

func (e *mineruCloudEngine) NewReader(_ context.Context, deps ReaderDeps) (interfaces.DocReader, error) {
	return NewMinerUCloudReader(deps.Overrides), nil
}
