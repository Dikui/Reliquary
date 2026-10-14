package modelcalls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	"reliquary/internal/types"
)

type capturedRequest struct {
	io.ReadCloser
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *capturedRequest) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.data.Write(p[:n])
	b.mu.Unlock()
	return n, err
}
func (b *capturedRequest) snapshot() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}
func captureEnabled(m *types.Model) bool {
	if m.Type == types.ModelTypeEmbedding {
		return os.Getenv("RELIQUARY_MODEL_CALL_CAPTURE_EMBEDDING") != "false"
	}
	if m.Type == types.ModelTypeRerank {
		return os.Getenv("RELIQUARY_MODEL_CALL_CAPTURE_RERANK") != "false"
	}
	return true
}
func requestSecrets(req *http.Request, m *types.Model) []string {
	secrets := []string{m.Parameters.APIKey, m.Parameters.AppSecret}
	for key, values := range req.Header {
		key = strings.ToLower(key)
		if key == "content-type" || key == "accept" || key == "user-agent" || key == "content-length" {
			continue
		}
		for _, value := range values {
			if key == "authorization" || key == "cookie" || strings.Contains(key, "key") || strings.Contains(key, "token") || strings.Contains(key, "auth") || m.Parameters.CustomHeaders[http.CanonicalHeaderKey(key)] != "" {
				secrets = append(secrets, value)
				if strings.HasPrefix(value, "Bearer ") {
					secrets = append(secrets, strings.TrimPrefix(value, "Bearer "))
				}
			}
		}
	}
	for key, value := range m.Parameters.CustomHeaders {
		if !strings.EqualFold(key, "content-type") && !strings.EqualFold(key, "accept") && !strings.EqualFold(key, "user-agent") {
			secrets = append(secrets, value)
		}
	}
	for key, values := range req.URL.Query() {
		if credentialKey(key) {
			secrets = append(secrets, values...)
		}
	}
	return secrets
}
func credentialKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	switch key {
	case "apikey", "authorization", "accesskey", "accesskeyid", "secretaccesskey", "secretkey", "token", "accesstoken", "refreshtoken", "password", "appsecret", "clientsecret", "cookie":
		return true
	}
	return false
}

var jsonFieldPrefix = regexp.MustCompile(`"(?:[^"\\]|\\.)*"\s*:\s*`)

// Decode field names so alternate Unicode escapes cannot conceal credentials.
func redactCredentialFields(raw string) string {
	indices := jsonFieldPrefix.FindAllStringIndex(raw, -1)
	for i := len(indices) - 1; i >= 0; i-- {
		at := indices[i]
		prefix := raw[at[0]:at[1]]
		colon := strings.LastIndex(prefix, ":")
		var key string
		if colon < 0 || json.Unmarshal([]byte(strings.TrimSpace(prefix[:colon])), &key) != nil || !credentialKey(key) {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(raw[at[1]:]))
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			continue
		}
		end := at[1] + int(decoder.InputOffset())
		raw = raw[:at[1]] + `"[REDACTED]"` + raw[end:]
	}
	return raw
}

var imageURI = regexp.MustCompile(`data:image/[a-zA-Z0-9.+-]+;base64,[a-zA-Z0-9+/=]+`)

// Substitutions preserve all other wire bytes, including JSON whitespace.
func protectContent(raw string, secrets []string, ctx context.Context) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		encoded, _ := json.Marshal(secret)
		raw = strings.ReplaceAll(raw, string(encoded[1:len(encoded)-1]), "[REDACTED]")
		raw = strings.ReplaceAll(raw, secret, "[REDACTED]")
	}
	raw = redactCredentialFields(raw)
	var walk func(json.RawMessage, bool)
	walk = func(node json.RawMessage, binary bool) {
		var object map[string]json.RawMessage
		if json.Unmarshal(node, &object) == nil && object != nil {
			for key, value := range object {
				if credentialKey(key) {
					encodedKey, _ := json.Marshal(key)
					pattern := regexp.MustCompile(`(` + regexp.QuoteMeta(string(encodedKey)) + `\s*:\s*)` + regexp.QuoteMeta(string(value)))
					raw = pattern.ReplaceAllString(raw, `${1}"[REDACTED]"`)
					continue
				}
				walk(value, binary || key == "images" || key == "data" && bytes.Contains(node, []byte(`"base64"`)))
			}
			return
		}
		var list []json.RawMessage
		if json.Unmarshal(node, &list) == nil && list != nil {
			for _, value := range list {
				walk(value, binary)
			}
			return
		}
		var value string
		if json.Unmarshal(node, &value) == nil {
			protected := value
			for _, secret := range secrets {
				if secret != "" {
					protected = strings.ReplaceAll(protected, secret, "[REDACTED]")
				}
			}
			if protected != value {
				encoded, _ := json.Marshal(protected)
				raw = strings.ReplaceAll(raw, string(node), string(encoded))
			}
		}
		if json.Unmarshal(node, &value) == nil && binary && !strings.Contains(value, "://") && !strings.HasPrefix(value, "data:") {
			if data, err := base64.StdEncoding.DecodeString(value); err == nil {
				reference := imageReference(data, ctx)
				encoded, _ := json.Marshal(reference)
				raw = strings.ReplaceAll(raw, string(node), string(encoded))
			}
		}
	}
	walk(json.RawMessage(raw), false)
	raw = imageURI.ReplaceAllStringFunc(raw, func(uri string) string {
		_, payload, _ := strings.Cut(uri, ",")
		data, _ := base64.StdEncoding.DecodeString(payload)
		return imageReference(data, ctx)
	})
	return raw
}
func imageReference(data []byte, ctx context.Context) string {
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	path := ""
	for _, image := range types.ModelCallImagesFromContext(ctx) {
		if image.SHA256 == hash {
			path = image.Path
			break
		}
	}
	if strings.HasPrefix(path, "data:") {
		path = ""
	}
	// Percent encoding prevents quotes and backslashes in paths changing wire JSON.
	if path != "" {
		path = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(path, "%", "%25"), "\"", "%22"), "\\", "%5C")
	}
	return "image-ref:sha256=" + hash + ";path=" + path
}
func vectorSummary(raw string) string {
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &object) != nil {
		return raw
	}
	var data []struct {
		Embedding []json.RawMessage `json:"embedding"`
	}
	var vectors [][]json.RawMessage
	var vector []json.RawMessage
	dimensions := []int{}
	if json.Unmarshal(object["data"], &data) == nil {
		for _, row := range data {
			if row.Embedding != nil {
				dimensions = append(dimensions, len(row.Embedding))
			}
		}
	}
	if json.Unmarshal(object["embeddings"], &vectors) == nil {
		for _, row := range vectors {
			dimensions = append(dimensions, len(row))
		}
	}
	if json.Unmarshal(object["embedding"], &vector) == nil && vector != nil {
		dimensions = append(dimensions, len(vector))
	}
	if len(dimensions) == 0 {
		return raw
	}
	encoded, _ := json.Marshal(map[string]any{"count": len(dimensions), "dimensions": dimensions, "usage": object["usage"], "vector_values_omitted": true})
	return string(encoded)
}
