package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// parseShelfVision renders one page at a time and discards all temporary
// bytes after the call; only the final page-marked Markdown is persisted.
func (s *EntityService) parseShelfVision(ctx context.Context, parser types.ShelfParser, original io.Reader) (string, error) {
	if s.models == nil {
		return "", fmt.Errorf("vision model service is unavailable")
	}
	metadata, err := s.models.GetModelByID(ctx, parser.ModelID)
	if err != nil {
		return "", err
	}
	if !metadata.SupportsImageProcessing() {
		return "", fmt.Errorf("selected parser model does not support images")
	}
	model, err := s.models.GetChatModel(ctx, parser.ModelID)
	if err != nil {
		return "", err
	}
	ctx = types.WithLLMCallMetadata(ctx, "shelf_parse", "")
	directory, err := os.MkdirTemp("", "reliquary-shelf-parse-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "original")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(file, original)
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	file, err = os.Open(path)
	if err != nil {
		return "", err
	}
	magic := make([]byte, 5)
	_, err = io.ReadFull(file, magic)
	file.Close()
	if err != nil {
		return "", fmt.Errorf("original is too short for visual parsing")
	}
	if string(magic) != "%PDF-" {
		file, err = os.Open(path)
		if err != nil {
			return "", err
		}
		defer file.Close()
		page, err := shelfVisionPage(ctx, model, parser.Prompt, 1, file)
		if err != nil {
			return "", err
		}
		return shelfPagesMarkdown([]shelfParsedPage{page})
	}
	renderCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	info, err := exec.CommandContext(renderCtx, "pdfinfo", path).Output()
	cancel()
	if err != nil {
		return "", fmt.Errorf("PDF inspection failed; install Poppler pdfinfo and provide an unencrypted valid PDF")
	}
	count := 0
	for _, line := range strings.Split(string(info), "\n") {
		if value, ok := strings.CutPrefix(line, "Pages:"); ok {
			count, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if count < 1 {
		return "", fmt.Errorf("PDF contains no readable pages")
	}
	pages := make([]shelfParsedPage, 0)
	total := 0
	for page := 1; page <= count; page++ {
		prefix := filepath.Join(directory, "page")
		dpi, err := shelfPDFRenderDPI(ctx, path, page, parser.RenderDPI)
		if err != nil {
			return "", err
		}
		renderCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = exec.CommandContext(renderCtx, "pdftoppm", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-r", strconv.FormatFloat(dpi, 'f', 3, 64), "-singlefile", "-png", path, prefix).Run()
		cancel()
		if err != nil {
			return "", fmt.Errorf("PDF page %d rendering failed; install Poppler pdftoppm", page)
		}
		rendered, err := os.Open(prefix + ".png")
		if err != nil {
			return "", err
		}
		result, err := shelfVisionPage(ctx, model, parser.Prompt, page, rendered)
		rendered.Close()
		os.Remove(prefix + ".png")
		if err != nil {
			return "", err
		}
		total += len(result.Markdown)
		if total > shelfParserMaxOutput {
			return "", fmt.Errorf("parser output is too large")
		}
		pages = append(pages, result)
	}
	return shelfPagesMarkdown(pages)
}

var shelfPDFPageSize = regexp.MustCompile(`(?m)^Page(?:\s+[0-9]+)?\s+size:\s+([0-9.]+) x ([0-9.]+) pts`)

// Honor the configured resolution up to the same image bounds used by models.
// Read page geometry before rendering so a huge PDF page cannot allocate an
// unbounded bitmap merely because a high DPI was configured.
func shelfPDFRenderDPI(ctx context.Context, path string, page, requested int) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := exec.CommandContext(ctx, "pdfinfo", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), path).Output()
	if err != nil {
		return 0, fmt.Errorf("PDF page geometry is unreadable")
	}
	match := shelfPDFPageSize.FindSubmatch(info)
	if len(match) != 3 {
		return 0, fmt.Errorf("PDF page geometry is missing")
	}
	width, _ := strconv.ParseFloat(string(match[1]), 64)
	height, _ := strconv.ParseFloat(string(match[2]), 64)
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("invalid PDF page size")
	}
	dpi := math.Min(float64(requested), 4096*72/math.Max(width, height))
	dpi = math.Min(dpi, math.Sqrt(8_000_000/(width*height))*72)
	if dpi < 0.001 {
		return 0, fmt.Errorf("PDF page is too large to render")
	}
	return dpi, nil
}

func shelfVisionPage(ctx context.Context, model chat.Chat, prompt string, page int, reader io.Reader) (shelfParsedPage, error) {
	prepared, err := utils.PrepareModelImage(ctx, reader)
	if err != nil {
		return shelfParsedPage{}, err
	}
	image := "data:" + http.DetectContentType(prepared.Data) + ";base64," + base64.StdEncoding.EncodeToString(prepared.Data)
	response, err := model.Chat(ctx, []chat.Message{
		{Role: "system", Content: "Transcribe this document page as Markdown. Preserve tables, numbers and original wording. Return only Markdown without page markers or a wrapping code fence. Do not infer unreadable or missing facts."},
		{Role: "user", Content: fmt.Sprintf("%s\n\nPage %d", prompt, page), Images: []string{image}},
	}, nil)
	if err != nil {
		return shelfParsedPage{}, err
	}
	if response == nil || response.FinishReason == "length" {
		return shelfParsedPage{}, fmt.Errorf("vision parser returned an incomplete page")
	}
	content := strings.TrimSpace(response.Content)
	if bytes.HasPrefix([]byte(content), []byte("```markdown\n")) && strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(strings.TrimPrefix(content, "```markdown\n"), "```")
	}
	return shelfParsedPage{Page: page, Markdown: content}, nil
}
