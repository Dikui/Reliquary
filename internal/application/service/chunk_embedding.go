package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// prepareChunkEmbeddingInput keeps each block and its saved images in one group.
// Source hashes cover originals; model-call references identify prepared copies.
func prepareChunkEmbeddingInput(ctx context.Context, model embedding.Embedder, content, body string, sourceOnly bool, readImage func(string) (io.ReadCloser, error)) (*types.IndexInfo, string, error) {
	input := &types.IndexInfo{Content: content}
	fusion, ok := model.(embedding.FusionEmbedder)
	if !ok || !fusion.SupportsFusion() {
		return input, vaultContentHash(content), nil
	}
	refs := []string{}
	for _, ref := range articleImagesInOrder(body, nil) {
		if remoteIngestImage(ref) || slices.Contains(refs, ref) {
			continue
		}
		refs = append(refs, ref)
	}
	if len(refs) > 10 {
		return nil, "", &utils.ImageInputError{Reason: "fusion embedding supports at most 10 images per block"}
	}
	sources := []string{content}
	for _, ref := range refs {
		file, err := readImage(ref)
		if err != nil {
			return nil, "", fmt.Errorf("read chunk image: %w", err)
		}
		var sourceHash string
		if sourceOnly {
			hash := sha256.New()
			_, err = io.Copy(hash, file)
			sourceHash = fmt.Sprintf("%x", hash.Sum(nil))
		} else {
			var prepared *utils.ModelImage
			prepared, err = utils.PrepareModelImage(ctx, file)
			if err == nil {
				sourceHash = prepared.SourceHash
				input.Images = append(input.Images, "data:"+http.DetectContentType(prepared.Data)+";base64,"+base64.StdEncoding.EncodeToString(prepared.Data))
				input.ImageReferences = append(input.ImageReferences, types.ModelCallImageReference{Path: ref, SHA256: fmt.Sprintf("%x", sha256.Sum256(prepared.Data))})
			}
		}
		file.Close()
		if err != nil {
			return nil, "", fmt.Errorf("read or prepare chunk image: %w", err)
		}
		sources = append(sources, ref, sourceHash)
	}
	if len(refs) == 0 {
		return input, vaultContentHash(content), nil
	}
	encoded, _ := json.Marshal(sources)
	return input, vaultContentHash(string(encoded)), nil
}

func (r *EmbeddingRebuilder) chunkEmbeddingInput(ctx context.Context, model embedding.Embedder, entry *types.Entry, content, body string, sourceOnly bool) (*types.IndexInfo, string, error) {
	return prepareChunkEmbeddingInput(ctx, model, content, body, sourceOnly, func(ref string) (io.ReadCloser, error) {
		if r.vault == nil {
			return nil, fmt.Errorf("image store unavailable")
		}
		return r.vault.ReadImage(entry, ref)
	})
}
