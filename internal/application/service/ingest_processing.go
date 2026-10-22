package service

import (
	"strings"

	"reliquary/internal/types"
)

// All ingested templates share body indexing and background enrichment.
func isIngestArticle(k *types.Entry) bool {
	return k != nil && (k.SourceKind == "" || k.SourceKind == "article" || k.SourceKind == "mine" || k.SourceKind == "report")
}

func remoteIngestImage(ref string) bool {
	return strings.HasPrefix(strings.ToLower(ref), "http://") || strings.HasPrefix(strings.ToLower(ref), "https://")
}
