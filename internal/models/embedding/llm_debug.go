package embedding

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reliquary/internal/logger"
)

// debugEmbedder wraps an Embedder with LLM debug logging.
type debugEmbedder struct {
	inner Embedder
}

func (d *debugEmbedder) SupportsFusion() bool {
	e, ok := d.inner.(FusionEmbedder)
	return ok && e.SupportsFusion()
}

func (d *debugEmbedder) EmbedFusion(ctx context.Context, text string, images []string) ([]float32, error) {
	e, ok := d.inner.(FusionEmbedder)
	if !ok || !e.SupportsFusion() {
		return nil, fmt.Errorf("embedding model does not support fusion")
	}
	start := time.Now()
	result, err := e.EmbedFusion(ctx, text, images)
	inputs := []string{text}
	for _, image := range images {
		inputs = append(inputs, fmt.Sprintf("image data URI (%d bytes)", len(image)))
	}
	logEmbeddingDebug(ctx, d.inner.GetModelName(), inputs, singleToDouble(result), err, time.Since(start))
	return result, err
}

func (d *debugEmbedder) GetProfileHash() string {
	if model, ok := d.inner.(ProfiledEmbedder); ok {
		return model.GetProfileHash()
	}
	return ""
}

func (d *debugEmbedder) SupportsImages() bool {
	e, ok := d.inner.(ImageEmbedder)
	return ok && e.SupportsImages()
}

func (d *debugEmbedder) EmbedImage(ctx context.Context, image string) ([]float32, error) {
	e, ok := d.inner.(ImageEmbedder)
	if !ok || !e.SupportsImages() {
		return nil, fmt.Errorf("embedding model does not support images")
	}
	start := time.Now()
	result, err := e.EmbedImage(ctx, image)
	logEmbeddingDebug(ctx, d.inner.GetModelName(), []string{fmt.Sprintf("image data URI (%d bytes)", len(image))}, singleToDouble(result), err, time.Since(start))
	return result, err
}

func (d *debugEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	start := time.Now()
	result, err := d.inner.Embed(ctx, text)
	logEmbeddingDebug(ctx, d.inner.GetModelName(), []string{text}, singleToDouble(result), err, time.Since(start))
	return result, err
}

func (d *debugEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	start := time.Now()
	result, err := d.inner.BatchEmbed(ctx, texts)
	logEmbeddingDebug(ctx, d.inner.GetModelName(), texts, result, err, time.Since(start))
	return result, err
}

func (d *debugEmbedder) BatchEmbedWithPool(ctx context.Context, model Embedder, texts []string) ([][]float32, error) {
	return d.inner.BatchEmbedWithPool(ctx, d, texts)
}

func (d *debugEmbedder) GetModelName() string { return d.inner.GetModelName() }
func (d *debugEmbedder) GetDimensions() int   { return d.inner.GetDimensions() }
func (d *debugEmbedder) GetModelID() string   { return d.inner.GetModelID() }

func singleToDouble(v []float32) [][]float32 {
	if v == nil {
		return nil
	}
	return [][]float32{v}
}

func logEmbeddingDebug(ctx context.Context, model string, inputs []string, outputs [][]float32, callErr error, dur time.Duration) {
	if !logger.LLMDebugEnabled() {
		return
	}

	record := &logger.LLMCallRecord{
		CallType: "Embedding",
		Model:    model,
		Duration: dur,
	}

	// Input section: show each text with a preview
	var inputBuf strings.Builder
	inputBuf.WriteString(fmt.Sprintf("count=%d\n", len(inputs)))
	for i, t := range inputs {
		preview := strings.ReplaceAll(t, "\n", "\\n")
		preview = logger.TruncateRunes(preview, 200)
		inputBuf.WriteString(fmt.Sprintf("[%d] (len=%d) %s\n", i, len([]rune(t)), preview))
	}
	record.Sections = append(record.Sections, logger.RecordSection{Title: "Input", Content: inputBuf.String()})

	// Output section
	if outputs != nil {
		var outBuf strings.Builder
		outBuf.WriteString(fmt.Sprintf("count=%d\n", len(outputs)))
		for i, vec := range outputs {
			if len(vec) > 0 {
				outBuf.WriteString(fmt.Sprintf("[%d] dims=%d, first_3=[%.6f, %.6f, %.6f]\n", i, len(vec),
					safeIdx(vec, 0), safeIdx(vec, 1), safeIdx(vec, 2)))
			} else {
				outBuf.WriteString(fmt.Sprintf("[%d] empty\n", i))
			}
		}
		record.Sections = append(record.Sections, logger.RecordSection{Title: "Output", Content: outBuf.String()})
	}

	if callErr != nil {
		record.Error = callErr.Error()
	}
	logger.LLMDebugLog(ctx, record)
}

func safeIdx(v []float32, i int) float32 {
	if i < len(v) {
		return v[i]
	}
	return 0
}
