package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type processingImageInput struct {
	Index int `json:"index"`
	Start int `json:"start"`
}

// Check before checkpoints or non-visual branches can skip a stored GIF.
func rejectGIFImages(body string, open func(string) (io.ReadCloser, error)) error {
	for i, ref := range bodyImageReferences(body) {
		if remoteIngestImage(ref.URL) {
			continue
		}
		file, err := open(ref.URL)
		if err != nil {
			continue // Missing and unreadable references keep their existing skip reason.
		}
		var header [6]byte
		n, _ := io.ReadFull(file, header[:])
		file.Close()
		if http.DetectContentType(header[:n]) == "image/gif" {
			return &utils.ImageInputError{Reason: fmt.Sprintf("第 %d 张图片不支持 GIF，请提供静态图片后重新处理", i+1)}
		}
	}
	return nil
}

func (s *entryService) attachProcessingImages(ctx context.Context, l *types.Library, k *types.Entry, body string, vision bool, supplied ...[]docparser.MarkdownImageReference) ([]string, []processingImageInput, []types.ProcessingImage, error) {
	images := []string{}
	inputs := []processingImageInput{}
	records := []types.ProcessingImage{}
	refs := bodyImageReferences(body)
	if len(supplied) > 0 {
		refs = supplied[0]
	}
	for i, ref := range refs {
		start := len([]rune(body[:ref.Position]))
		row := types.ProcessingImage{URL: ref.URL, Ordinal: i + 1, Locations: bodyRangeLocations(body, start, start+len([]rune(ref.URL)), "image_description")}
		if !vision {
			row.SkipReason = "处理模型不支持视觉"
		} else {
			uri, err := s.processingImageDataURI(ctx, l, k, ref.URL)
			if err != nil {
				if utils.IsPermanentImageError(err) {
					return nil, nil, records, err
				}
				row.SkipReason = err.Error()
			} else {
				images = append(images, uri)
				inputs = append(inputs, processingImageInput{Index: i + 1, Start: start})
			}
		}
		records = append(records, row)
	}
	return images, inputs, records, nil
}

func qualifyImageLocations(body string, p types.ProcessingSegment, part *articleProcessingResult, supplied ...[]docparser.MarkdownImageReference) {
	refs := bodyImageReferences(body)
	local := bodyImageReferences(string([]rune(body)[p.Start:p.End]))
	if len(supplied) > 0 {
		local = supplied[0]
	}
	for i := range part.Images {
		if i >= len(local) {
			continue
		}
		start := p.Start + len([]rune(string([]rune(body)[p.Start:p.End])[:local[i].Position]))
		part.Images[i].Locations = bodyRangeLocations(body, start, start+len([]rune(local[i].URL)), "image_description")
		for j, ref := range refs {
			if len([]rune(body[:ref.Position])) == start {
				part.Images[i].Ordinal = j + 1
				break
			}
		}
	}
}

func imageDescriptionChunks(k *types.Entry, images []types.ProcessingImage, startIndex int) []*types.Chunk {
	result := []*types.Chunk{}
	for _, image := range images {
		if image.Description == "" || image.SkipReason != "" {
			continue
		}
		infos, _ := json.Marshal([]types.ImageInfo{{URL: image.URL, Caption: image.Description}})
		c := &types.Chunk{ID: uuid.NewString(), SpaceID: k.SpaceID, EntryID: k.ID, LibraryID: k.LibraryID, Content: image.Description, ChunkType: types.ChunkTypeImageDescription, ChunkIndex: startIndex + len(result), IsEnabled: true, ImageInfo: string(infos), SourceLocations: image.Locations, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if len(image.Locations) > 0 {
			c.StartAt = image.Locations[0].Start
			c.EndAt = image.Locations[len(image.Locations)-1].End
		}
		result = append(result, c)
	}
	return result
}

// Image counts also constrain segment boundaries so every readable image is sent.
func imageBoundedSegments(body string, plan []types.ProcessingSegment, imageLimit, segmentLimit int) ([]types.ProcessingSegment, error) {
	refs := bodyImageReferences(body)
	runes := []rune(body)
	out := []types.ProcessingSegment{}
	for _, p := range plan {
		start, count := p.Start, 0
		for _, ref := range refs {
			at := len([]rune(body[:ref.Position]))
			if at < p.Start || at >= p.End {
				continue
			}
			if count == imageLimit {
				next := p
				boundary := imageSyntaxBoundary(body, ref.Position)
				next.Start, next.End = start, boundary
				out = append(out, next)
				start = boundary
				count = 0
			}
			count++
		}
		p.Start = start
		out = append(out, p)
	}
	if len(out) > segmentLimit {
		return nil, &types.SegmentLimitError{Count: len(out), Limit: segmentLimit}
	}
	for i := range out {
		out[i].Number = i + 1
		out[i].Hash = fmt.Sprintf("%x", sha256.Sum256([]byte(string(runes[out[i].Start:out[i].End]))))
	}
	return out, nil
}

func imageSyntaxBoundary(body string, position int) int {
	prefix := body[:position]
	markdown := strings.LastIndex(prefix, "![")
	html := strings.LastIndex(strings.ToLower(prefix), "<img")
	at := max(markdown, html)
	if at < 0 {
		at = position
	}
	return len([]rune(body[:at]))
}

// References belong to the segment containing their destination start even when
// a very long paragraph is split inside Markdown image syntax.
func segmentImageReferences(body string, p types.ProcessingSegment) []docparser.MarkdownImageReference {
	refs := []docparser.MarkdownImageReference{}
	runes := []rune(body)
	for _, ref := range bodyImageReferences(body) {
		start := len([]rune(body[:ref.Position]))
		if start >= p.Start && start < p.End {
			ref.Position = len(string(runes[p.Start:start]))
			refs = append(refs, ref)
		}
	}
	return refs
}
