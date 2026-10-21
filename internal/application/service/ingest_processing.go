package service

import (
	"strings"

	"reliquary/internal/types"
)

// isIngestArticle covers articles and imported user documents. Reports keep
// their separate indexing path without intake review or model processing.
func isIngestArticle(k *types.Entry) bool {
	return k != nil && (k.SourceKind == "" || k.SourceKind == "article" || k.SourceKind == "mine")
}

func remoteIngestImage(ref string) bool {
	return strings.HasPrefix(strings.ToLower(ref), "http://") || strings.HasPrefix(strings.ToLower(ref), "https://")
}
