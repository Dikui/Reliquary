package service

import (
	"encoding/json"
	"strings"

	"reliquary/internal/types"
)

// isIngestArticle also recognizes articles collected before acquisition existed.
func isIngestArticle(k *types.Entry) bool {
	if k == nil {
		return false
	}
	if k.Acquisition != "" {
		return true
	}
	intake, _ := k.IntakeMetadata()
	return intake != nil && intake.Source == "ingest"
}

func remoteIngestImage(ref string) bool {
	return strings.HasPrefix(strings.ToLower(ref), "http://") || strings.HasPrefix(strings.ToLower(ref), "https://")
}

// A recorded missing reference remains in the original body. This substitution
// is only for validation, never for article storage or processing input.
func recordedMissingImageBody(k *types.Entry, body string) string {
	if !isIngestArticle(k) {
		return body
	}
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
