package service

import (
	"encoding/json"
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

// A recorded missing reference remains in the original body. This substitution
// is only for validation, never for article storage or processing input.
func recordedMissingImageBody(k *types.Entry, body string) string {
	var missing []types.IngestMissing
	if json.Unmarshal(k.MissingMaterials, &missing) != nil {
		return body
	}
	for _, item := range missing {
		if item.Type == "image" && item.SourceURL != "" {
			body = strings.ReplaceAll(body, item.SourceURL, "local://missing/image")
		}
	}
	return body
}
