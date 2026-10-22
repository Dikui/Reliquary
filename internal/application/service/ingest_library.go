package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func (s *entryService) resolveIngestLibrary(ctx context.Context, selector string) (*types.Library, error) {
	libraries, err := s.libraryService.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	for _, library := range libraries {
		if library.ID == strings.TrimSpace(selector) || library.Directory == strings.TrimSpace(selector) {
			return library, nil
		}
	}
	return nil, werrors.NewBadRequestError("library is missing or does not exist; call list_libraries for available libraries")
}

var identifierScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)

func validateLibraryIngest(request *types.IngestRequest, library *types.Library) error {
	bad := werrors.NewBadRequestError
	if utf8.RuneCountInString(request.SourceRef) > types.MaxIngestSourceRefRunes {
		return bad("source_ref exceeds 2048 Unicode code points")
	}
	if strings.ContainsRune(request.SourceRef, 0) || strings.ContainsRune(request.Identifier, 0) {
		return bad("source_ref and identifier cannot contain NUL")
	}
	request.SourceRef = strings.TrimSpace(request.SourceRef)
	if request.IdentifierProvided || request.Identifier != "" {
		if utf8.RuneCountInString(request.Identifier) > types.MaxIngestIdentifierRunes {
			return bad("identifier exceeds 2048 Unicode code points")
		}
		scheme, value, ok := strings.Cut(strings.TrimSpace(request.Identifier), ":")
		scheme, value = strings.TrimSpace(scheme), strings.TrimSpace(value)
		if !ok || !identifierScheme.MatchString(scheme) || value == "" {
			return bad("identifier must be scheme:value")
		}
		scheme = strings.ToLower(scheme)
		if scheme == "doi" {
			value = strings.ToLower(value)
		}
		request.Identifier = scheme + ":" + value
	}
	return nil
}

// Source identity compares the submitted Markdown text, independently of approximate external-source deduplication.
func sourceBodyHash(body string) string {
	normalized := strings.Join(strings.Fields(utils.CleanMarkdown(body)), " ")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalized)))
}
