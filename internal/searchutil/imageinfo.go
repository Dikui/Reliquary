package searchutil

import (
	"context"
	"encoding/json"
	"regexp"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// MarkdownImageRegex matches Markdown image links: ![alt](url)
var MarkdownImageRegex = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// HTMLImageSrcRegex matches an HTML <img> tag with a quoted src attribute.
// Documents that rely on surrounding markup for layout embed their screenshots
// this way, so image references must be matched back to these tags as well
// as to Markdown image links.
//
// Submatches: 1 = attributes before src, 2 = the src value, 3 = attributes
// after. Callers index the value through HTMLImageSrcURLGroup rather than
// assuming a position.
//
// src must be preceded by whitespace so that data-src and other hyphenated
// attribute names are not mistaken for it — matching those would capture a
// lazy-loading placeholder and leave the real src unvisited. An unquoted src
// and a srcset-only tag are deliberately out of scope; both are rare in
// authored documents and matching them reliably needs an HTML parser.
//
// Both this package and the document parser share this one definition. Two
// copies would be free to drift, and an image the parser stores but the
// enricher cannot match back is exactly the kind of gap this exists to close.
var HTMLImageSrcRegex = regexp.MustCompile(`(?i)<img\b([^>]*?)\ssrc\s*=\s*['"]([^'"]+)['"]([^>]*)>`)

// HTMLImageSrcURLGroup is the submatch index of the src value in
// HTMLImageSrcRegex.
const HTMLImageSrcURLGroup = 2

// CollectImageInfoByChunkIDs reads image references stored on enabled body chunks.
// Parent chunks inherit references from their enabled text children.
func CollectImageInfoByChunkIDs(ctx context.Context, chunkRepo interfaces.ChunkRepository, spaceID uint64, chunkIDs []string) map[string]string {
	out := make(map[string]string)
	chunks, err := chunkRepo.ListChunksByID(ctx, spaceID, chunkIDs)
	if err != nil {
		return out
	}
	var parentIDs []string
	for _, chunk := range chunks {
		if !chunk.IsEnabled {
			continue
		}
		if chunk.ImageInfo != "" {
			out[chunk.ID] = chunk.ImageInfo
		}
		if chunk.ChunkType == types.ChunkTypeParentText {
			parentIDs = append(parentIDs, chunk.ID)
		}
	}
	if len(parentIDs) > 0 {
		children, err := chunkRepo.ListChunksByParentIDs(ctx, spaceID, parentIDs)
		if err == nil {
			groups := make(map[string]map[string]string)
			for _, child := range children {
				if !child.IsEnabled || child.ChunkType != types.ChunkTypeText || child.ImageInfo == "" {
					continue
				}
				if groups[child.ParentChunkID] == nil {
					groups[child.ParentChunkID] = make(map[string]string)
				}
				groups[child.ParentChunkID][child.ID] = child.ImageInfo
			}
			for id, infos := range groups {
				out[id] = MergeImageInfoJSON(infos)
			}
		}
	}
	return out
}

// EnrichSearchResultsImageInfo fills in ImageInfo for SearchResults that have
// none by batch-querying body chunks and their text children.
func EnrichSearchResultsImageInfo(
	ctx context.Context,
	chunkRepo interfaces.ChunkRepository,
	spaceID uint64,
	results []*types.SearchResult,
) {
	var chunkIDs []string
	seen := make(map[string]bool)
	for _, r := range results {
		if r.ImageInfo != "" {
			continue
		}
		if !seen[r.ID] {
			seen[r.ID] = true
			chunkIDs = append(chunkIDs, r.ID)
		}
	}
	if len(chunkIDs) == 0 {
		return
	}

	infoMap := CollectImageInfoByChunkIDs(ctx, chunkRepo, spaceID, chunkIDs)
	if len(infoMap) == 0 {
		return
	}

	for _, r := range results {
		if r.ImageInfo != "" {
			continue
		}
		if merged, ok := infoMap[r.ID]; ok {
			r.ImageInfo = merged
		}
	}
}

// MergeImageInfoJSON combines per-chunk image_info JSON strings (from
// CollectImageInfoByChunkIDs) into a single JSON array, deduplicating by URL.
func MergeImageInfoJSON(perChunk map[string]string) string {
	if len(perChunk) == 0 {
		return ""
	}
	seen := make(map[string]bool)
	var all []types.ImageInfo
	for _, raw := range perChunk {
		var infos []types.ImageInfo
		if err := json.Unmarshal([]byte(raw), &infos); err != nil {
			continue
		}
		for _, info := range infos {
			key := info.URL
			if key == "" {
				key = info.OriginalURL
			}
			if key != "" && !seen[key] {
				seen[key] = true
				all = append(all, info)
			}
		}
	}
	if len(all) == 0 {
		return ""
	}
	data, err := json.Marshal(all)
	if err != nil {
		return ""
	}
	return string(data)
}
