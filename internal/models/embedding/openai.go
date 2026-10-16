package embedding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reliquary/internal/logger"
	secutils "reliquary/internal/utils"
)

// OpenAIEmbedder implements text vectorization functionality using OpenAI API
type OpenAIEmbedder struct {
	apiKey                    string
	baseURL                   string
	modelName                 string
	truncatePromptTokens      int
	dimensions                int
	modelID                   string
	httpClient                *http.Client
	timeout                   time.Duration
	maxRetries                int
	customHeaders             map[string]string
	supportsDimensionOverride bool
	supportsImages            bool
	multimodalFormat          string
	EmbedderPooler
}

// DashScope serves multimodal embeddings on its native API, not under the
// OpenAI-compatible base URL, and caps one request at 20 content elements.
const (
	dashscopeMultimodalPath   = "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"
	dashscopeMaxContents      = 20
	multimodalFormatDashScope = "dashscope"
)

type dashscopeEmbedRequest struct {
	Model string `json:"model"`
	Input struct {
		Contents []multimodalEmbedInput `json:"contents"`
	} `json:"input"`
	Parameters *dashscopeEmbedParameters `json:"parameters,omitempty"`
}

type dashscopeEmbedParameters struct {
	Dimension int `json:"dimension"`
}

type dashscopeEmbedResponse struct {
	Output struct {
		Embeddings []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"embeddings"`
	} `json:"output"`
}

type multimodalEmbedInput struct {
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
}

type multimodalEmbedRequest struct {
	Model          string                 `json:"model"`
	Input          []multimodalEmbedInput `json:"input"`
	EncodingFormat string                 `json:"encoding_format"`
	Dimensions     int                    `json:"dimensions,omitempty"`
}

func (e *OpenAIEmbedder) SupportsImages() bool { return e.supportsImages }

func (e *OpenAIEmbedder) EmbedImage(ctx context.Context, image string) ([]float32, error) {
	if !e.supportsImages {
		return nil, fmt.Errorf("embedding model does not support images")
	}
	header, payload, found := strings.Cut(image, ",")
	if !found || !strings.HasPrefix(header, "data:image/") || !strings.HasSuffix(header, ";base64") {
		return nil, fmt.Errorf("image embedding input must be an image base64 data URI")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("image embedding input contains invalid or empty base64")
	}
	vectors, err := e.multimodalEmbed(ctx, []multimodalEmbedInput{{Image: image}})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (e *OpenAIEmbedder) multimodalEmbed(ctx context.Context, inputs []multimodalEmbedInput) ([][]float32, error) {
	dashscope := e.multimodalFormat == multimodalFormatDashScope
	if dashscope && len(inputs) > dashscopeMaxContents {
		vectors := make([][]float32, 0, len(inputs))
		for start := 0; start < len(inputs); start += dashscopeMaxContents {
			part, err := e.multimodalEmbed(ctx, inputs[start:min(start+dashscopeMaxContents, len(inputs))])
			if err != nil {
				return nil, err
			}
			vectors = append(vectors, part...)
		}
		return vectors, nil
	}
	var data []byte
	var err error
	if dashscope {
		request := dashscopeEmbedRequest{Model: e.modelName}
		request.Input.Contents = inputs
		if e.supportsDimensionsParam() {
			request.Parameters = &dashscopeEmbedParameters{Dimension: e.dimensions}
		}
		data, err = json.Marshal(request)
	} else {
		request := multimodalEmbedRequest{Model: e.modelName, Input: inputs, EncodingFormat: "float"}
		if e.supportsDimensionsParam() {
			request.Dimensions = e.dimensions
		}
		data, err = json.Marshal(request)
	}
	if err != nil {
		return nil, fmt.Errorf("marshal multimodal embedding request: %w", err)
	}
	response, err := e.doRequestWithRetry(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("send multimodal embedding request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read multimodal embedding response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		if len(body) > 1000 {
			body = body[:1000]
		}
		return nil, fmt.Errorf("multimodal embedding HTTP %d: %s", response.StatusCode, body)
	}
	var result OpenAIEmbedResponse
	if dashscope {
		var native dashscopeEmbedResponse
		err = json.Unmarshal(body, &native)
		result.Data = native.Output.Embeddings
	} else {
		err = json.Unmarshal(body, &result)
	}
	if err != nil {
		return nil, fmt.Errorf("decode multimodal embedding response: %w", err)
	}
	if len(result.Data) != len(inputs) {
		return nil, fmt.Errorf("multimodal embedding count mismatch: got %d, expected %d", len(result.Data), len(inputs))
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(vectors) || vectors[item.Index] != nil {
			return nil, fmt.Errorf("multimodal embedding response has invalid or duplicate index %d", item.Index)
		}
		if len(item.Embedding) == 0 || (e.dimensions > 0 && len(item.Embedding) != e.dimensions) {
			return nil, fmt.Errorf("multimodal embedding dimension mismatch: got %d, expected %d", len(item.Embedding), e.dimensions)
		}
		var norm float64
		for _, value := range item.Embedding {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, fmt.Errorf("multimodal embedding contains a non-finite value")
			}
			norm += float64(value) * float64(value)
		}
		if norm == 0 {
			return nil, fmt.Errorf("multimodal embedding has zero length")
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

// OpenAIEmbedRequest represents an OpenAI embedding request
type OpenAIEmbedRequest struct {
	Model                string   `json:"model"`
	Input                []string `json:"input"`
	EncodingFormat       string   `json:"encoding_format,omitempty"`
	Dimensions           int      `json:"dimensions,omitempty"`
	TruncatePromptTokens int      `json:"truncate_prompt_tokens,omitempty"`
}

// OpenAIEmbedResponse represents an OpenAI embedding response
type OpenAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// NewOpenAIEmbedder creates a new OpenAI embedder
func NewOpenAIEmbedder(apiKey, baseURL, modelName string,
	truncatePromptTokens int, dimensions int, modelID string, pooler EmbedderPooler,
) (*OpenAIEmbedder, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	baseURL = strings.TrimRight(baseURL, "/")

	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	if truncatePromptTokens == 0 {
		truncatePromptTokens = 511
	}

	timeout := 60 * time.Second

	if err := validateEmbeddingBaseURL(baseURL); err != nil {
		return nil, err
	}

	return &OpenAIEmbedder{
		apiKey:               apiKey,
		baseURL:              baseURL,
		modelName:            modelName,
		httpClient:           newEmbeddingHTTPClient(timeout),
		truncatePromptTokens: truncatePromptTokens,
		EmbedderPooler:       pooler,
		dimensions:           dimensions,
		modelID:              modelID,
		timeout:              timeout,
		maxRetries:           3, // Maximum retry count
	}, nil
}

// SetCustomHeaders 设置用户自定义 HTTP 请求头（类似 OpenAI Python SDK 的 extra_headers）。
// 保留头（Authorization、Content-Type 等）会在发送时被自动跳过。
func (e *OpenAIEmbedder) SetCustomHeaders(headers map[string]string) {
	e.customHeaders = headers
}

func (e *OpenAIEmbedder) SetSupportsDimensionOverride(supported bool) {
	e.supportsDimensionOverride = supported
}

// Embed converts text to vector
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	for range 3 {
		embeddings, err := e.BatchEmbed(ctx, []string{text})
		if err != nil {
			return nil, err
		}
		if len(embeddings) > 0 {
			return embeddings[0], nil
		}
	}
	return nil, fmt.Errorf("no embedding returned")
}

func (e *OpenAIEmbedder) doRequestWithRetry(ctx context.Context, jsonData []byte) (*http.Response, error) {
	var resp *http.Response
	var err error
	endpoint := e.baseURL + "/embeddings"
	if e.multimodalFormat == multimodalFormatDashScope {
		if base, parseErr := url.Parse(e.baseURL); parseErr == nil {
			endpoint = base.Scheme + "://" + base.Host + dashscopeMultimodalPath
		}
	}

	for i := 0; i <= e.maxRetries; i++ {
		if i > 0 {
			backoffTime := time.Duration(1<<uint(i-1)) * time.Second
			if backoffTime > 10*time.Second {
				backoffTime = 10 * time.Second
			}
			logger.GetLogger(ctx).
				Infof("OpenAIEmbedder retrying request (%d/%d), waiting %v", i, e.maxRetries, backoffTime)

			select {
			case <-time.After(backoffTime):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Rebuild request each time to ensure Body is valid.
		// IMPORTANT: declare `req` separately (var) so the assignment to `err`
		// below uses the outer-scope variable, not a fresh loop-local one.
		// Previously this read `req, err := http.NewRequestWithContext(...)`,
		// where `:=` introduced a new `err` shadowing the outer one. The
		// `resp, err = httpClient.Do(req)` line then wrote to the shadowed
		// `err` only, so when all retries failed with connection errors the
		// outer `err` stayed nil. The function returned `(nil, nil)`, and
		// callers (BatchEmbed line 195) blindly dereferenced `resp.Body` →
		// SIGSEGV nil-pointer panic that took down the whole process.
		// Reproduce: stop the embedding upstream (e.g. localhost:3130), make
		// any RAG query → backend SIGSEGV instead of returning HTTP 500.
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonData))
		if err != nil {
			logger.GetLogger(ctx).Errorf("OpenAIEmbedder failed to create request: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
		secutils.ApplyCustomHeaders(req, e.customHeaders)

		resp, err = e.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}

		logger.GetLogger(ctx).Errorf("OpenAIEmbedder request failed (attempt %d/%d): %v", i+1, e.maxRetries+1, err)
	}

	return nil, err
}

func (e *OpenAIEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if e.supportsImages {
		if len(texts) == 0 {
			return [][]float32{}, nil
		}
		inputs := make([]multimodalEmbedInput, len(texts))
		for index, text := range texts {
			if strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("multimodal text embedding input must not be empty")
			}
			inputs[index].Text = text
		}
		return e.multimodalEmbed(ctx, inputs)
	}
	// Create request body
	reqBody := OpenAIEmbedRequest{
		Model:                e.modelName,
		Input:                texts,
		EncodingFormat:       "float",
		TruncatePromptTokens: e.truncatePromptTokens,
	}
	if e.supportsDimensionsParam() {
		reqBody.Dimensions = e.dimensions
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		logger.GetLogger(ctx).Errorf("OpenAIEmbedder EmbedBatch marshal request error: %v", err)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Log request details for debugging
	logger.GetLogger(ctx).Debugf("OpenAIEmbedder BatchEmbed: model=%s, input_count=%d, truncate_tokens=%d",
		e.modelName, len(texts), e.truncatePromptTokens)

	// Check for invalid input lengths and log details
	hasInvalidLength := false
	for i, text := range texts {
		textLen := len(text)
		textPreview := text
		if len(textPreview) > 200 {
			textPreview = textPreview[:200] + "..."
		}

		// Log warning if length is outside valid range [1, 8192]
		if textLen == 0 || textLen > 8192 {
			hasInvalidLength = true
			logger.GetLogger(ctx).Errorf("OpenAIEmbedder BatchEmbed input[%d]: INVALID length=%d (must be [1, 8192]), preview=%s",
				i, textLen, textPreview)
		} else {
			logger.GetLogger(ctx).Debugf("OpenAIEmbedder BatchEmbed input[%d]: length=%d, preview=%s",
				i, textLen, textPreview)
		}
	}

	if hasInvalidLength {
		logger.GetLogger(ctx).Errorf("OpenAIEmbedder BatchEmbed: Found invalid input lengths, this will likely cause API error")
	}

	// Send request (passing jsonData instead of constructing http.Request)
	resp, err := e.doRequestWithRetry(ctx, jsonData)
	if err != nil {
		logger.GetLogger(ctx).Errorf("OpenAIEmbedder EmbedBatch send request error: %v", err)
		return nil, fmt.Errorf("send request: %w", err)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.GetLogger(ctx).Errorf("OpenAIEmbedder EmbedBatch read response error: %v", err)
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Log detailed error response from OpenAI API
		bodyStr := string(body)
		if len(bodyStr) > 1000 {
			bodyStr = bodyStr[:1000] + "... (truncated)"
		}
		logger.GetLogger(ctx).Errorf("OpenAIEmbedder EmbedBatch API error: Http Status %s, Response Body: %s", resp.Status, bodyStr)
		return nil, fmt.Errorf("EmbedBatch API error: Http Status %s, Response: %s", resp.Status, bodyStr)
	}

	// Parse response
	var response OpenAIEmbedResponse
	if err := json.Unmarshal(body, &response); err != nil {
		logger.GetLogger(ctx).Errorf("OpenAIEmbedder EmbedBatch unmarshal response error: %v", err)
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	// Extract embedding vectors
	embeddings := make([][]float32, 0, len(response.Data))
	for _, data := range response.Data {
		embeddings = append(embeddings, data.Embedding)
	}

	return embeddings, nil
}

// GetModelName returns the model name
func (e *OpenAIEmbedder) GetModelName() string {
	return e.modelName
}

func (e *OpenAIEmbedder) supportsDimensionsParam() bool {
	return e.supportsDimensionOverride && e.dimensions > 0
}

// GetDimensions returns the vector dimensions
func (e *OpenAIEmbedder) GetDimensions() int {
	return e.dimensions
}

// GetModelID returns the model ID
func (e *OpenAIEmbedder) GetModelID() string {
	return e.modelID
}
